package devicecontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

type recordedCall struct {
	method string
	params json.RawMessage
}

type fakeRunner struct {
	online bool
	calls  []recordedCall
	reply  map[string]json.RawMessage
}

func (f *fakeRunner) Online(string) bool { return f.online }

func (f *fakeRunner) Call(_ context.Context, _, method string, params any) (json.RawMessage, error) {
	p, _ := json.Marshal(params)
	f.calls = append(f.calls, recordedCall{method: method, params: p})
	if r, ok := f.reply[method]; ok {
		return r, nil
	}
	return json.RawMessage(`{}`), nil
}

// TestMCPDriverMapping verifies the desktop driver maps our primitives to the
// server-agnostic tunnel vocabulary and decodes screenshots.
func TestMCPDriverMapping(t *testing.T) {
	png := base64.StdEncoding.EncodeToString([]byte("IMG"))
	fr := &fakeRunner{online: true, reply: map[string]json.RawMessage{
		"screenshot": json.RawMessage(`{"pngBase64":"` + png + `"}`),
	}}
	d := mcpDriver{runners: fr, inst: domain.Instance{ID: "dev1"}}
	ctx := context.Background()

	img, err := d.Screenshot(ctx)
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	if string(img) != "IMG" {
		t.Fatalf("screenshot bytes = %q, want IMG", img)
	}

	if err := d.Tap(ctx, 12, 34); err != nil {
		t.Fatal(err)
	}
	if err := d.Text(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	if err := d.Key(ctx, "ENTER"); err != nil {
		t.Fatal(err)
	}

	methods := map[string]json.RawMessage{}
	for _, c := range fr.calls {
		methods[c.method] = c.params
	}
	if _, ok := methods["click"]; !ok {
		t.Fatal("no click call recorded")
	}
	var click map[string]int
	_ = json.Unmarshal(methods["click"], &click)
	if click["x"] != 12 || click["y"] != 34 {
		t.Fatalf("click params = %s, want x=12 y=34", methods["click"])
	}
	if _, ok := methods["type"]; !ok {
		t.Fatal("no type call recorded")
	}
	if _, ok := methods["key"]; !ok {
		t.Fatal("no key call recorded")
	}
}
