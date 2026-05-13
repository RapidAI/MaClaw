//go:build windows

package guiautomation

import (
	"fmt"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type windowsInputSimulator struct{}

func NewInputSimulator() InputSimulator { return &windowsInputSimulator{} }

func runInputPS(script string) error {
	cmd := coretool.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("input simulation failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

const u32Decl = "Add-Type @'\nusing System; using System.Runtime.InteropServices;\npublic class U32 {\n  [DllImport(\"user32.dll\")] public static extern bool SetCursorPos(int X,int Y);\n  [DllImport(\"user32.dll\")] public static extern void mouse_event(uint f,int dx,int dy,int d,IntPtr e);\n  [DllImport(\"user32.dll\")] public static extern void keybd_event(byte vk,byte sc,uint f,IntPtr e);\n  [DllImport(\"user32.dll\")] public static extern int GetSystemMetrics(int i);\n  public static int ClampX(int x){int w=GetSystemMetrics(0);if(x<0)return 0;if(x>=w)return w-1;return x;}\n  public static int ClampY(int y){int h=GetSystemMetrics(1);if(y<0)return 0;if(y>=h)return h-1;return y;}\n}\n'@; "

func moveThen(x, y int, ev string) string {
return u32Decl + fmt.Sprintf("$x=[U32]::ClampX(%d);$y=[U32]::ClampY(%d);[U32]::SetCursorPos($x,$y);%s", x, y, ev)
}

func (w *windowsInputSimulator) Click(x, y int) error {
return runInputPS(moveThen(x, y, "[U32]::mouse_event(0x0002,0,0,0,[IntPtr]::Zero);[U32]::mouse_event(0x0004,0,0,0,[IntPtr]::Zero)"))
}

func (w *windowsInputSimulator) RightClick(x, y int) error {
return runInputPS(moveThen(x, y, "[U32]::mouse_event(0x0008,0,0,0,[IntPtr]::Zero);[U32]::mouse_event(0x0010,0,0,0,[IntPtr]::Zero)"))
}

func (w *windowsInputSimulator) DoubleClick(x, y int) error {
c := "[U32]::mouse_event(0x0002,0,0,0,[IntPtr]::Zero);[U32]::mouse_event(0x0004,0,0,0,[IntPtr]::Zero)"
return runInputPS(moveThen(x, y, c+";Start-Sleep -Milliseconds 50;"+c))
}

func (w *windowsInputSimulator) Type(text string) error {
esc := strings.NewReplacer("+","{+}","^","{^}","%","{%}","~","{~}","(","{(}",")","{)}","{","{{}","}","{}}").Replace(text)
esc = strings.ReplaceAll(esc, "'", "''")
return runInputPS(fmt.Sprintf("Add-Type -AssemblyName System.Windows.Forms;[System.Windows.Forms.SendKeys]::SendWait('%s')", esc))
}
var vkMap = map[string]byte{
"ctrl":0x11,"control":0x11,"alt":0x12,"shift":0x10,"win":0x5B,"tab":0x09,"enter":0x0D,"return":0x0D,
"esc":0x1B,"escape":0x1B,"backspace":0x08,"delete":0x2E,"del":0x2E,"space":0x20,"insert":0x2D,
"home":0x24,"end":0x23,"pageup":0x21,"pagedown":0x22,"printscreen":0x2C,
"up":0x26,"down":0x28,"left":0x25,"right":0x27,
"f1":0x70,"f2":0x71,"f3":0x72,"f4":0x73,"f5":0x74,"f6":0x75,"f7":0x76,"f8":0x77,"f9":0x78,"f10":0x79,"f11":0x7A,"f12":0x7B,
}

func resolveVK(key string) (byte, error) {
k := strings.ToLower(strings.TrimSpace(key))
if vk, ok := vkMap[k]; ok { return vk, nil }
if len(k) == 1 {
c := k[0]
if c >= 'a' && c <= 'z' { return c - 32, nil }
if c >= '0' && c <= '9' { return c, nil }
}
return 0, fmt.Errorf("unknown key: %q", key)
}

func (w *windowsInputSimulator) KeyCombo(keys ...string) error {
if len(keys) == 0 { return nil }
var downs, ups []string
for _, k := range keys {
vk, err := resolveVK(k)
if err != nil { return err }
downs = append(downs, fmt.Sprintf("[U32]::keybd_event(0x%02X,0,0,[IntPtr]::Zero)", vk))
ups = append([]string{fmt.Sprintf("[U32]::keybd_event(0x%02X,0,0x0002,[IntPtr]::Zero)", vk)}, ups...)
}
return runInputPS(u32Decl + strings.Join(downs, ";") + ";" + strings.Join(ups, ";"))
}

func (w *windowsInputSimulator) Scroll(x, y, deltaX, deltaY int) error {
if deltaY == 0 { return nil }
return runInputPS(moveThen(x, y, fmt.Sprintf("[U32]::mouse_event(0x0800,0,0,%d,[IntPtr]::Zero)", deltaY*120)))
}

func (w *windowsInputSimulator) DragDrop(fromX, fromY, toX, toY int) error {
s := u32Decl + fmt.Sprintf("$fx=[U32]::ClampX(%d);$fy=[U32]::ClampY(%d);$tx=[U32]::ClampX(%d);$ty=[U32]::ClampY(%d);"+
"[U32]::SetCursorPos($fx,$fy);Start-Sleep -Milliseconds 50;"+
"[U32]::mouse_event(0x0002,0,0,0,[IntPtr]::Zero);Start-Sleep -Milliseconds 50;"+
"[U32]::SetCursorPos($tx,$ty);Start-Sleep -Milliseconds 50;"+
"[U32]::mouse_event(0x0004,0,0,0,[IntPtr]::Zero)", fromX, fromY, toX, toY)
return runInputPS(s)
}