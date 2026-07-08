package devicecontrol

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// Point is a screen coordinate in device pixels.
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// UINode is a compacted node of the uiautomator accessibility hierarchy. Only
// fields useful for reasoning and grounding are retained. Center is the tap
// target derived from bounds.
type UINode struct {
	Text        string   `json:"text,omitempty"`
	ResourceID  string   `json:"resourceId,omitempty"`
	Class       string   `json:"class,omitempty"`
	ContentDesc string   `json:"contentDesc,omitempty"`
	Package     string   `json:"package,omitempty"`
	Clickable   bool     `json:"clickable,omitempty"`
	Scrollable  bool     `json:"scrollable,omitempty"`
	Focused     bool     `json:"focused,omitempty"`
	Bounds      string   `json:"bounds,omitempty"`
	Center      *Point   `json:"center,omitempty"`
	Children    []UINode `json:"children,omitempty"`
}

// PerfSnapshot is a lightweight performance sample.
type PerfSnapshot struct {
	Package      string `json:"package,omitempty"`
	BatteryLevel int    `json:"batteryLevel"`
	TotalPSSKB   int    `json:"totalPssKb,omitempty"`
}

// xmlNode mirrors the raw uiautomator XML node schema for unmarshalling.
type xmlNode struct {
	Text        string    `xml:"text,attr"`
	ResourceID  string    `xml:"resource-id,attr"`
	Class       string    `xml:"class,attr"`
	ContentDesc string    `xml:"content-desc,attr"`
	Package     string    `xml:"package,attr"`
	Clickable   string    `xml:"clickable,attr"`
	Scrollable  string    `xml:"scrollable,attr"`
	Focused     string    `xml:"focused,attr"`
	Bounds      string    `xml:"bounds,attr"`
	Nodes       []xmlNode `xml:"node"`
}

type xmlHierarchy struct {
	Nodes []xmlNode `xml:"node"`
}

// ParseUIHierarchy converts raw `uiautomator dump` output into a compact tree.
// The dump tool appends a trailing status line after the XML, so the XML
// document is isolated first.
func ParseUIHierarchy(raw []byte) (*UINode, error) {
	xmlDoc := isolateXML(string(raw))
	if xmlDoc == "" {
		return nil, fmt.Errorf("no UI hierarchy found in dump output")
	}
	var h xmlHierarchy
	if err := xml.Unmarshal([]byte(xmlDoc), &h); err != nil {
		return nil, fmt.Errorf("parse ui hierarchy: %w", err)
	}
	root := &UINode{Class: "hierarchy"}
	for _, n := range h.Nodes {
		root.Children = append(root.Children, convertNode(n))
	}
	return root, nil
}

// isolateXML extracts the <?xml ...>...</hierarchy> document from the dump,
// discarding any leading/trailing noise (e.g. "UI hierchary dumped to: ...").
func isolateXML(s string) string {
	start := strings.Index(s, "<?xml")
	if start < 0 {
		start = strings.Index(s, "<hierarchy")
	}
	if start < 0 {
		return ""
	}
	end := strings.LastIndex(s, "</hierarchy>")
	if end < 0 {
		return ""
	}
	return s[start : end+len("</hierarchy>")]
}

func convertNode(n xmlNode) UINode {
	out := UINode{
		Text:        strings.TrimSpace(n.Text),
		ResourceID:  n.ResourceID,
		Class:       n.Class,
		ContentDesc: strings.TrimSpace(n.ContentDesc),
		Package:     n.Package,
		Clickable:   n.Clickable == "true",
		Scrollable:  n.Scrollable == "true",
		Focused:     n.Focused == "true",
		Bounds:      n.Bounds,
	}
	if c, ok := centerOfBounds(n.Bounds); ok {
		out.Center = &c
	}
	for _, child := range n.Nodes {
		out.Children = append(out.Children, convertNode(child))
	}
	return out
}

// centerOfBounds parses a "[x1,y1][x2,y2]" bounds string into its center point.
func centerOfBounds(bounds string) (Point, bool) {
	// Expected form: [left,top][right,bottom]
	b := strings.NewReplacer("[", " ", "]", " ", ",", " ").Replace(bounds)
	fields := strings.Fields(b)
	if len(fields) != 4 {
		return Point{}, false
	}
	nums := make([]int, 4)
	for i, f := range fields {
		v, err := strconv.Atoi(f)
		if err != nil {
			return Point{}, false
		}
		nums[i] = v
	}
	return Point{X: (nums[0] + nums[2]) / 2, Y: (nums[1] + nums[3]) / 2}, true
}

// parseResumedActivity extracts the focused component from `dumpsys activity
// activities` output (looks for the ResumedActivity / mResumedActivity line).
func parseResumedActivity(dump string) string {
	for _, line := range strings.Split(dump, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "ResumedActivity") || strings.Contains(line, "mCurrentFocus") {
			// e.g. "ResumedActivity: ActivityRecord{... com.pkg/.Activity ...}"
			if comp := extractComponent(line); comp != "" {
				return comp
			}
		}
	}
	return ""
}

// extractComponent finds a "pkg/activity" token within a line.
func extractComponent(line string) string {
	for _, tok := range strings.Fields(strings.NewReplacer("{", " ", "}", " ").Replace(line)) {
		if strings.Contains(tok, "/") && strings.Contains(tok, ".") && !strings.Contains(tok, "://") {
			return strings.TrimSuffix(tok, "}")
		}
	}
	return ""
}

// parseBatteryLevel reads the "level:" field from `dumpsys battery`.
func parseBatteryLevel(dump string) int {
	for _, line := range strings.Split(dump, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "level:") {
			if v, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "level:"))); err == nil {
				return v
			}
		}
	}
	return -1
}

// parseTotalPSS reads the "TOTAL" PSS value (KB) from `dumpsys meminfo <pkg>`.
func parseTotalPSS(dump string) int {
	for _, line := range strings.Split(dump, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.HasPrefix(fields[0], "TOTAL") {
			if v, err := strconv.Atoi(fields[1]); err == nil {
				return v
			}
		}
	}
	return 0
}
