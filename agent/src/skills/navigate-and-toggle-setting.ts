import { defineSkill } from "@flue/runtime";

export const navigateAndToggleSetting = defineSkill({
  name: "navigate-and-toggle-setting",
  description:
    "Open the device Settings, find a specific setting (scrolling or searching as needed), then read or change it and verify the new state. Activate for tasks like 'turn on airplane mode', 'set brightness to 50%', or 'check whether Bluetooth is on'.",
  instructions: `# Navigating settings and toggling a value

## 1. Open Settings
open_app {name:"Settings"} -> wait {seconds:1} -> ask_screen {question:"Is the Settings app open?"} to confirm.

## 2. Find the setting
- If a search field is visible, tap it, type the setting name (type_text), and pick the matching result. Prefer this - it is the most reliable path.
- Otherwise tap the most relevant top-level category, then scroll {direction:"down"} in steps, calling ask_screen after each scroll to check whether the target is visible. Bound the search to about 5 scrolls before trying a different category.

## 3. Read or change it
- To READ: ask_screen with a precise question ("What is the brightness percentage?" / "Is Wi-Fi on or off?").
- To CHANGE a toggle: ask_screen FIRST to read the current state. Only tap the toggle if it is not already in the desired state (so you never flip it the wrong way).
- For a slider: tap or drag toward the target, then re-read.

## 4. Verify
wait {seconds:1} -> ask_screen again to confirm the new state matches the request. Report the before -> after.

## Rules
- Read the current state before changing it - never assume a toggle's position.
- Use only setting names and labels you actually see on screen.`,
});
