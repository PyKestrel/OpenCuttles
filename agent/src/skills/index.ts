import type { Skill } from "@flue/runtime";
import { runUiTest } from "./run-ui-test.ts";
import { navigateAndToggleSetting } from "./navigate-and-toggle-setting.ts";
import { enterText } from "./enter-text.ts";
import { findAndTap } from "./find-and-tap.ts";
import { recoverWhenStuck } from "./recover-when-stuck.ts";
import { windowsDesktop } from "./windows-desktop.ts";
import { androidDevice } from "./android-device.ts";

// Lazy-loaded procedural playbooks. Flue lists each skill's name + description in
// the system prompt and adds the activate_skill tool; the model pulls a skill's
// full instructions into context on demand. The always-on core (identity, tool
// table, iron rules, the loop) lives in the agent's own instructions so even a
// model that never calls activate_skill still functions.
export const deviceSkills: Skill[] = [
  runUiTest,
  navigateAndToggleSetting,
  enterText,
  findAndTap,
  recoverWhenStuck,
  windowsDesktop,
  androidDevice,
];
