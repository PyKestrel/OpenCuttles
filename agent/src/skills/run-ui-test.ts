import { defineSkill } from "@flue/runtime";

// Core testing playbook: turn a natural-language test into observe/act/assert
// steps and emit a structured PASS/FAIL with on-screen evidence.
export const runUiTest = defineSkill({
  name: "run-ui-test",
  description:
    "Run a natural-language UI test: perform each step on the device, check the expected outcomes, and report a structured PASS/FAIL with on-screen evidence. Activate when the user gives you a test, a scenario, or steps with expected results to verify.",
  instructions: `# Running a UI test

Goal: execute a test written in plain language and report whether it passed, with evidence read from the screen.

## 1. Parse the test into steps
Split the request into an ordered list of ACTIONS (do X) and ASSERTIONS (expect Y). If the expected result isn't explicit, infer a reasonable pass condition and state it in your report.

## 2. Establish the starting point
- get_active_device -> note the platform (android / windows / linux / macos).
- ask_screen {question:"What app and screen is currently shown?"} -> record the starting state.

## 3. Execute each step, verifying as you go
For every ACTION:
1. Do exactly one tool call (open_app / tap_element / type_text / press_key / scroll).
2. wait {seconds:1}.
3. ask_screen to confirm the action had the intended effect. If it didn't, retry once; if it still fails, that step FAILS - stop and record where.

For every ASSERTION:
1. ask_screen with a precise question that returns the fact to check (e.g. "Is airplane mode on?" / "What number is in the total field?").
2. Compare the answer to the expected result. Record PASS or FAIL for that assertion, with the actual observed value.

## 4. Report (always end with this)
- RESULT: PASS or FAIL (FAIL if any assertion failed or any action could not be completed).
- STEPS: one line each - pass/fail mark, what you did, what you observed.
- EVIDENCE: the exact ask_screen answers you relied on for assertions.
- If FAIL: the first step that failed, and the actual vs. expected state.

## Rules
- Never mark an assertion PASS from memory - read the screen for each one.
- Do not skip or reorder steps, and do not substitute a different app or value.
- Never type real credentials. If a test needs a password or card number, stop and report that a human must enter it.`,
});
