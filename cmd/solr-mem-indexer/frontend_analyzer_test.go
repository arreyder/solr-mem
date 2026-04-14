package main

import (
	"testing"
)

func TestExtractSelectors_IntlFormatMessage(t *testing.T) {
	content := []byte(`
import { useIntl, FormattedMessage } from 'react-intl'

export function DenyPrimaryAction({ task }) {
  const intl = useIntl()
  const denyLabel = intl.formatMessage({ id: 'deny_action', defaultMessage: 'Deny' })

  return (
    <C1Button
      aria-label={intl.formatMessage({ id: 'deny_btn', defaultMessage: 'Deny task' })}
    >
      <FormattedMessage defaultMessage="Deny request" id="deny_request" />
    </C1Button>
  )
}
`)
	cs := extractSelectors("components/DenyPrimaryAction.tsx", content)

	if cs.ComponentName != "DenyPrimaryAction" {
		t.Errorf("expected component name DenyPrimaryAction, got %q", cs.ComponentName)
	}

	found := map[string][]string{}
	for _, s := range cs.Selectors {
		found[s.Type] = append(found[s.Type], s.Value)
	}

	// aria-label from intl.formatMessage
	if !contains(found["aria-label"], "Deny task") {
		t.Errorf("expected aria-label 'Deny task', got %v", found["aria-label"])
	}

	// intl-message from standalone intl.formatMessage
	if !contains(found["intl-message"], "Deny") {
		t.Errorf("expected intl-message 'Deny', got %v", found["intl-message"])
	}

	// intl-message from FormattedMessage
	if !contains(found["intl-message"], "Deny request") {
		t.Errorf("expected intl-message 'Deny request', got %v", found["intl-message"])
	}

	// "Deny task" should NOT appear as intl-message (it's an aria-label)
	if contains(found["intl-message"], "Deny task") {
		t.Errorf("'Deny task' should be aria-label only, not intl-message")
	}
}

func TestExtractSelectors_MultilineFormattedMessage(t *testing.T) {
	content := []byte(`
export function PolicyStep() {
  return (
    <div>
      <FormattedMessage
        defaultMessage="If unable to identify the selected assignee, fallback to group"
        id="component_policy_details_steps_group_fallback"
      />
    </div>
  )
}
`)
	cs := extractSelectors("components/PolicyStep.tsx", content)

	found := map[string][]string{}
	for _, s := range cs.Selectors {
		found[s.Type] = append(found[s.Type], s.Value)
	}

	if !contains(found["intl-message"], "If unable to identify the selected assignee, fallback to group") {
		t.Errorf("expected multiline FormattedMessage extraction, got %v", found["intl-message"])
	}
}

func TestExtractSelectors_AriaHasPopup(t *testing.T) {
	content := []byte(`
export function C1MoreButton({ menuId, open }) {
  return (
    <IconButton
      aria-controls={open ? menuId : undefined}
      aria-haspopup="true"
      aria-expanded={open}
      aria-label="More"
    >
      <MoreVertIcon />
    </IconButton>
  )
}
`)
	cs := extractSelectors("components/C1MoreButton.tsx", content)

	found := map[string][]string{}
	for _, s := range cs.Selectors {
		found[s.Type] = append(found[s.Type], s.Value)
	}

	if !contains(found["aria-haspopup"], "true") {
		t.Errorf("expected aria-haspopup 'true', got %v", found["aria-haspopup"])
	}
	if !contains(found["aria-label"], "More") {
		t.Errorf("expected aria-label 'More', got %v", found["aria-label"])
	}
}

func TestExtractSelectors_TabID(t *testing.T) {
	content := []byte(`
export function TopBarV2({ tabs }) {
  return (
    <TabList>
      {tabs.items.map((t, idx) => (
        <TabV2
          key={` + "`${tabs.menuKey}-tab-${idx}`" + `}
          id={` + "`${tabs.menuKey}-tab-${idx}`" + `}
          aria-controls={` + "`${tabs.menuKey}-tabpanel-${idx}`" + `}
          data-testid={` + "`${tabs.menuKey}-tab-${t.key}`" + `}
          label={t.title}
        />
      ))}
    </TabList>
  )
}
`)
	cs := extractSelectors("components/TopBarV2.tsx", content)

	found := map[string][]string{}
	for _, s := range cs.Selectors {
		found[s.Type] = append(found[s.Type], s.Value)
	}

	if len(found["tab-id"]) < 2 {
		t.Errorf("expected at least 2 tab-id selectors (id + tabpanel), got %v", found["tab-id"])
	}
}

func TestExtractSelectors_AriaLabelIntlNotDuplicated(t *testing.T) {
	content := []byte(`
export function EditButton() {
  const intl = useIntl()
  return (
    <C1IconButton
      aria-label={intl.formatMessage({ id: 'edit_rule', defaultMessage: 'Edit rule' })}
    >
      <Pencil />
    </C1IconButton>
  )
}
`)
	cs := extractSelectors("components/EditButton.tsx", content)

	ariaCount := 0
	intlCount := 0
	for _, s := range cs.Selectors {
		if s.Type == "aria-label" && s.Value == "Edit rule" {
			ariaCount++
		}
		if s.Type == "intl-message" && s.Value == "Edit rule" {
			intlCount++
		}
	}

	if ariaCount != 1 {
		t.Errorf("expected exactly 1 aria-label 'Edit rule', got %d", ariaCount)
	}
	if intlCount != 0 {
		t.Errorf("expected 0 intl-message 'Edit rule' (should be aria-label only), got %d", intlCount)
	}
}

func TestExtractSelectors_YAMLOutput(t *testing.T) {
	content := []byte(`
export function TestComponent() {
  const intl = useIntl()
  return (
    <div>
      <C1Button
        aria-label={intl.formatMessage({ id: 'btn', defaultMessage: 'Submit' })}
        aria-haspopup="menu"
      >
        <FormattedMessage defaultMessage="Save changes" id="save" />
      </C1Button>
    </div>
  )
}
`)
	cs := extractSelectors("components/TestComponent.tsx", content)
	yaml := buildSelectorYAML(cs)

	// Verify key sections exist.
	for _, section := range []string{"aria-label:", "aria-haspopup:", "intl-message:", "element:"} {
		if !containsStr(yaml, section) {
			t.Errorf("YAML output missing section %q:\n%s", section, yaml)
		}
	}
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

func containsStr(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
