import { defineSkill } from "@flue/runtime";

export const recoverWhenStuck = defineSkill({
  name: "recover-when-stuck",
  description:
    "Diagnose and recover when a tool returns an error, the screen is unexpected, or the same step keeps failing. Activate whenever you are stuck, looping, or a tool call errored.",
  instructions: `# Recovering when stuck

Work through this in order.

## 1. Read the error text literally
Tool errors are directive - they usually name the fix ("call list_apps", "this tool is Android-only, use ask_screen", "don't guess a deviceId"). Do exactly what it says. Do NOT invent a workaround, a new id, or a new package/app name.

## 2. Re-ground yourself
- get_active_device -> confirm the platform. Many loops come from treating a desktop like Android (or vice-versa); make sure you're using platform-appropriate tools.
- ask_screen {question:"What app and screen is shown right now?"} -> the real state is often not what you assumed.

## 3. Common causes -> fixes
- "unknown method" or tool errors on a desktop -> you used an Android-only tool (launch_app, get_ui_tree). Use open_app / ask_screen instead.
- open_app opened the wrong app -> call list_apps, find the EXACT name, and open_app again with it.
- a tap did nothing -> the element wasn't there or wasn't tappable; use the find-and-tap approach (scroll, re-describe).
- an app didn't open -> wait {seconds:2} and re-check; it may still be loading.
- a made-up deviceId error -> omit deviceId; you already act on the active device.

## 4. Don't repeat a failing action
If the same step failed twice, do something DIFFERENT (different tool, scroll, re-open the app). Never issue the identical call a third time.

## 5. Know when to stop
If you can't make progress after re-grounding and two different approaches, STOP and report plainly: the goal, the last screen you saw (quote ask_screen), and exactly what blocked you. A clear failure report beats flailing.`,
});
