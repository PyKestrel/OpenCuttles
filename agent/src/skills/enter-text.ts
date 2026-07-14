import { defineSkill } from "@flue/runtime";

export const enterText = defineSkill({
  name: "enter-text",
  description:
    "Reliably type text into a specific field: focus the field, type, and verify the text landed. Activate whenever a task needs text entered - search boxes, address bars, forms, message fields. Never for real passwords.",
  instructions: `# Entering text into a field

## Steps
1. ask_screen {question:"Which input fields are visible and which one is focused?"} to locate the target field.
2. tap_element {description:"<the specific field, e.g. 'the search box at the top'>"} to focus it.
3. ask_screen {question:"Is the <field> now focused with a cursor?"} - confirm focus BEFORE typing. If not focused, tap again.
4. type_text {text:"<text>"}.
5. ask_screen {question:"What text now appears in the <field>?"} - confirm it landed correctly. If it is wrong or partial, clear it (press_key {key:"BACKSPACE"} as needed) and retype.
6. If the task requires submitting: press_key {key:"ENTER"} (or tap the explicit submit/search button), then ask_screen to confirm the result.

## Rules
- One field at a time; always confirm focus before typing and confirm the value after.
- Watch for autocomplete/autosuggest replacing your text - re-read after typing.
- NEVER type real passwords, card numbers, or other secret credentials. If a step needs one, stop and report that a human must enter it.`,
});
