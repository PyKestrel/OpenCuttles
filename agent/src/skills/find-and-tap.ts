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
4. ANDROID ONLY - the deterministic fallback (use this whenever vision keeps hitting the WRONG element, e.g. a nearby label instead of a switch): call get_ui_tree, find the target by its label / resource-id, read its bounds [x1,y1][x2,y2], and call tap {x:(x1+x2)/2, y:(y1+y2)/2}. get_ui_tree bounds are in the same coordinate space tap uses, so this hits the exact element with no vision guesswork. Prefer this over re-describing the element to tap_element - passing text to tap_element still goes through vision.
5. DESKTOP: if it is a menu or toolbar item, consider a keyboard path (press_key) instead of tapping.

## After tapping
Always ask_screen to confirm the tap did what you expected before moving on. If nothing changed, do NOT immediately switch to a different element - first retry the SAME target once (a single tap can fail to register), and on Android escalate to the get_ui_tree + tap {x,y} path in rung 4 before assuming you had the wrong element.

## When to stop
If after the ladder the element still isn't found, STOP and report: what you were looking for, what IS on screen (from ask_screen), and that the target wasn't present. Do not tap a random element and never invent coordinates.`,
});
