import { defineSkill } from "@flue/runtime";

export const windowsDesktop = defineSkill({
  name: "windows-desktop",
  description:
    "Playbook for driving a Windows desktop: opening apps via the Start menu, window focus, keyboard shortcuts and chords, right-click/double-click, wheel scrolling, and which tools do and don't work there. Activate when get_active_device reports platform 'windows'.",
  instructions: `# Driving a Windows desktop

## What works / what doesn't
- WORKS: open_app, list_apps, current_activity, ask_screen, tap_element, tap, click, press_chord, find_element, type_text, press_key, scroll, wait.
- DOES NOT (Android-only, will error): launch_app (there are no packages - use open_app {name}); get_ui_tree (no accessibility tree here - use ask_screen).
- There is NO Home / Back / App-switch button. Don't press_key HOME/BACK/APP_SWITCH.

## Opening apps
- open_app {name:"Settings"} / {name:"Notepad"} / {name:"Chrome"} - opens the Start menu, types the name, and launches the match.
- The result reports which app it opened ("opened <Name>") - verify it is the one you wanted. If it opened the wrong app, call list_apps to get exact Start-menu names, then retry with the exact name.

## Mouse
- scroll {direction:"down"} turns a REAL mouse wheel here, so it works on maps, canvases, and custom lists that ignore keyboard paging. Add amount to go further ({amount:10}), or x/y to scroll a specific pane instead of the screen centre - hover the pane you mean, not the window edge.
- Right-click (context menus): click {x,y,button:"right"}. Double-click (open an item): click {x,y,count:2}. Locate the point first with find_element, or take an element's centre from a screenshot.

## Keyboard
- press_chord for combinations: {keys:["CTRL","C"]} copy, {keys:["CTRL","V"]} paste, {keys:["CTRL","A"]} select all, {keys:["ALT","TAB"]} switch window, {keys:["ALT","F4"]} close window, {keys:["WIN","R"]} Run dialog. Modifiers (CTRL/ALT/SHIFT/WIN) come FIRST, the real key LAST.
- press_key for single keys: ENTER, TAB, ESC, F1-F12, arrows, PAGEDOWN/PAGEUP.
- Keyboard is often the most reliable path on desktop: a shortcut beats hunting for a small toolbar button.

## Navigation
- current_activity returns the foreground window title - use it to confirm which window is focused.
- press_key {key:"WIN"} opens the Start menu; press_key {key:"ESC"} closes menus and dialogs.

## Common patterns
- Dialogs (Save As, permission prompts, UAC): read them with ask_screen, then tap_element the explicit button ("the Save button"). Never assume a dialog dismissed - confirm with ask_screen.
- Desktop apps can take a moment to launch: wait {seconds:1} and ask_screen to confirm readiness rather than assuming it opened instantly.`,
});
