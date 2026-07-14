import { defineSkill } from "@flue/runtime";

export const androidDevice = defineSkill({
  name: "android-device",
  description:
    "Playbook for driving an Android phone: launcher, HOME/BACK/APP_SWITCH navigation, opening apps by name or package, the accessibility tree, and permission dialogs. Activate when get_active_device reports platform 'android'.",
  instructions: `# Driving an Android device

## What works
Full toolset: open_app, launch_app, ask_screen, tap_element, find_element, type_text, press_key, scroll, get_ui_tree, list_apps, current_activity, wait.

## Opening apps
- Prefer open_app {name:"Settings"} (by display name).
- If you have the EXACT package, launch_app {package:"com.android.settings"} also works. If you don't know the package, call list_apps - never guess a package name.

## Navigation keys (press_key)
- HOME -> launcher. BACK -> back one screen. APP_SWITCH -> recent apps. ENTER / TAB / arrows as usual.
- Use BACK to leave a wrong screen instead of getting stuck.

## When vision struggles
get_ui_tree returns the accessibility tree as JSON - read exact labels / resource-ids, then tap_element using that exact text. Good for lists, small controls, and ambiguous icons.

## Common patterns
- Permission dialogs ("Allow X to ..."): read with ask_screen, then tap the explicit choice ("the Allow button").
- After launching an app, wait {seconds:1} and ask_screen to confirm it is on the expected screen before acting.
- current_activity returns the resumed package/activity - use it to confirm which app is foreground.`,
});
