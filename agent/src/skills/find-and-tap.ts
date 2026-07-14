import { defineSkill } from "@flue/runtime";

export const findAndTap = defineSkill({
  name: "find-and-tap",
  description:
    "Locate and tap an on-screen element that may be off-screen, small, or ambiguous, using scroll and accessibility-tree fallbacks. Activate when tap_element fails, the target isn't visible, or you're unsure an element is present.",
  instructions: `# Finding and tapping a stubborn element

## Ladder (stop as soon as one rung works)
1. find_element {description:"<precise description>"} - if found, tap_element with the same description.
2. If not found, it may be off-screen: scroll {direction:"down"} once, then find_element again. Repeat up to about 5 times. Try {direction:"up"} if you may have scrolled past it.
3. Make the description more specific and visual - reference nearby text, position, icon shape, or color ("the blue Save button at the bottom-right", "the gear icon next to 'Display'").
4. ANDROID ONLY: if vision still can't find it, call get_ui_tree, read the exact label / resource-id, then tap_element using that exact text.
5. DESKTOP: if it is a menu or toolbar item, consider a keyboard path (press_key) instead of tapping.

## After tapping
Always ask_screen to confirm the tap did what you expected before moving on.

## When to stop
If after the ladder the element still isn't found, STOP and report: what you were looking for, what IS on screen (from ask_screen), and that the target wasn't present. Do not tap a random element and never invent coordinates.`,
});
