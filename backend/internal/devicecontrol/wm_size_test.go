package devicecontrol
import "testing"
func TestParseWmSize(t *testing.T) {
  w,h,err := ParseWMSize("Physical size: 720x1280\nOverride size: 1080x1920\n")
  if err!=nil||w!=1080||h!=1920 { t.Fatalf("override: %d %d %v",w,h,err) }
  w,h,_ = ParseWMSize("Physical size: 1440x3120")
  if w!=1440||h!=3120 { t.Fatalf("physical: %d %d",w,h) }
  if _,_,e:=ParseWMSize("garbage"); e==nil { t.Fatal("expected error") }
}
