// Maclaw UIA sidecar — long-lived UI Automation process.
// Protocol: one JSON object per line on stdin; one JSON response per line on stdout.
//
// Build (Framework 4.x):
//   csc /nologo /t:exe /out:maclaw-uia-sidecar.exe ^
//       /r:UIAutomationClient.dll /r:UIAutomationTypes.dll /r:WindowsBase.dll Program.cs
//
// Ops: ping | enum | find  (same shape as the PowerShell fallback sidecar)

using System;
using System.Collections.Generic;
using System.Text;
using System.Windows.Automation;
// Minimal JSON writer/parser for our fixed shapes (no System.Web.Extensions).

namespace MaclawUIASidecar
{
    class Program
    {
        static void Main(string[] args)
        {
            Console.InputEncoding = Encoding.UTF8;
            Console.OutputEncoding = Encoding.UTF8;
            string line;
            while ((line = Console.ReadLine()) != null)
            {
                line = line.Trim();
                if (line.Length == 0) continue;
                try
                {
                    var req = JsonMini.ParseObject(line);
                    string op = JsonMini.GetString(req, "op");
                    if (op == "ping")
                    {
                        WriteOk(new Dictionary<string, object> { { "pong", true } });
                    }
                    else if (op == "enum")
                    {
                        string window = JsonMini.GetString(req, "window");
                        int depth = JsonMini.GetInt(req, "depth", 3);
                        if (depth < 1) depth = 1;
                        if (depth > 5) depth = 5;
                        var els = EnumWindows(window, depth);
                        WriteOk(new Dictionary<string, object> { { "elements", els } });
                    }
                    else if (op == "find")
                    {
                        string window = JsonMini.GetString(req, "window");
                        string role = JsonMini.GetString(req, "role");
                        string name = JsonMini.GetString(req, "name");
                        var el = FindEl(window, role, name);
                        WriteOk(new Dictionary<string, object> { { "element", el } });
                    }
                    else
                    {
                        WriteErr("unknown op: " + op);
                    }
                }
                catch (Exception ex)
                {
                    WriteErr(ex.Message);
                }
            }
        }

        static void WriteOk(Dictionary<string, object> extra)
        {
            var d = new Dictionary<string, object> { { "ok", true } };
            if (extra != null)
            {
                foreach (var kv in extra) d[kv.Key] = kv.Value;
            }
            Console.WriteLine(JsonMini.Serialize(d));
        }

        static void WriteErr(string msg)
        {
            Console.WriteLine(JsonMini.Serialize(new Dictionary<string, object> {
                { "ok", false }, { "error", msg ?? "" }
            }));
        }

        static List<object> EnumWindows(string window, int depth)
        {
            var root = AutomationElement.RootElement;
            var list = new List<object>();
            if (string.IsNullOrEmpty(window))
            {
                var wins = root.FindAll(TreeScope.Children, Condition.TrueCondition);
                foreach (AutomationElement w in wins)
                {
                    string name = SafeName(w);
                    if (string.IsNullOrEmpty(name)) continue;
                    list.Add(NodeFromElement(w, 1)); // top-level only
                }
                return list;
            }
            var win = FindWindowBySubstring(window);
            if (win == null) return list;
            list.Add(NodeFromElement(win, depth));
            return list;
        }

        static Dictionary<string, object> FindEl(string window, string role, string name)
        {
            var win = FindWindowBySubstring(window);
            if (win == null) return null;
            Condition cond;
            var nameCond = new PropertyCondition(AutomationElement.NameProperty, name ?? "");
            ControlType ct = MapRole(role);
            if (ct != null)
            {
                var typeCond = new PropertyCondition(AutomationElement.ControlTypeProperty, ct);
                cond = new AndCondition(typeCond, nameCond);
            }
            else
            {
                cond = nameCond;
            }
            var el = win.FindFirst(TreeScope.Descendants, cond);
            if (el == null) return null;
            return NodeFromElement(el, 1);
        }

        static AutomationElement FindWindowBySubstring(string window)
        {
            if (string.IsNullOrEmpty(window)) return null;
            var root = AutomationElement.RootElement;
            var wins = root.FindAll(TreeScope.Children, Condition.TrueCondition);
            string needle = window.ToLowerInvariant();
            foreach (AutomationElement w in wins)
            {
                string n = SafeName(w);
                if (!string.IsNullOrEmpty(n) && n.ToLowerInvariant().Contains(needle))
                    return w;
            }
            return null;
        }

        static Dictionary<string, object> NodeFromElement(AutomationElement el, int depth)
        {
            var rect = el.Current.BoundingRectangle;
            string role = "";
            try { role = el.Current.ControlType.ProgrammaticName.Replace("ControlType.", ""); }
            catch { }
            string val = "";
            try
            {
                object pat;
                if (el.TryGetCurrentPattern(ValuePattern.Pattern, out pat))
                {
                    var vp = pat as ValuePattern;
                    if (vp != null) val = vp.Current.Value ?? "";
                }
            }
            catch { }

            var node = new Dictionary<string, object>
            {
                { "role", role },
                { "name", SafeName(el) },
                { "value", val },
                { "x", (int)rect.X },
                { "y", (int)rect.Y },
                { "width", (int)rect.Width },
                { "height", (int)rect.Height },
            };
            if (depth > 1)
            {
                var kids = new List<object>();
                try
                {
                    var children = el.FindAll(TreeScope.Children, Condition.TrueCondition);
                    foreach (AutomationElement c in children)
                    {
                        kids.Add(NodeFromElement(c, depth - 1));
                    }
                }
                catch { }
                if (kids.Count > 0) node["children"] = kids;
            }
            return node;
        }

        static string SafeName(AutomationElement el)
        {
            try { return el.Current.Name ?? ""; }
            catch { return ""; }
        }

        static ControlType MapRole(string role)
        {
            if (string.IsNullOrEmpty(role)) return null;
            switch (role.ToLowerInvariant())
            {
                case "button": return ControlType.Button;
                case "edit":
                case "textfield": return ControlType.Edit;
                case "text": return ControlType.Text;
                case "checkbox": return ControlType.CheckBox;
                case "combobox": return ControlType.ComboBox;
                case "list": return ControlType.List;
                case "listitem": return ControlType.ListItem;
                case "menu": return ControlType.Menu;
                case "menuitem": return ControlType.MenuItem;
                case "tab": return ControlType.Tab;
                case "tabitem": return ControlType.TabItem;
                case "tree": return ControlType.Tree;
                case "treeitem": return ControlType.TreeItem;
                case "window": return ControlType.Window;
                case "radiobutton": return ControlType.RadioButton;
                case "slider": return ControlType.Slider;
                case "hyperlink": return ControlType.Hyperlink;
                default: return null; // ControlType is a reference type
            }
        }
    }

    // Minimal JSON for our fixed request/response shapes (no external deps).
    static class JsonMini
    {
        public static Dictionary<string, object> ParseObject(string json)
        {
            // Very small parser for flat objects: {"op":"enum","window":"x","depth":3}
            var d = new Dictionary<string, object>(StringComparer.OrdinalIgnoreCase);
            if (string.IsNullOrEmpty(json)) return d;
            json = json.Trim();
            if (json.Length < 2 || json[0] != '{') return d;
            int i = 1;
            while (i < json.Length)
            {
                SkipWs(json, ref i);
                if (i >= json.Length || json[i] == '}') break;
                if (json[i] == ',') { i++; continue; }
                string key = ReadString(json, ref i);
                SkipWs(json, ref i);
                if (i < json.Length && json[i] == ':') i++;
                SkipWs(json, ref i);
                object val = ReadValue(json, ref i);
                if (key != null) d[key] = val;
            }
            return d;
        }

        public static string GetString(Dictionary<string, object> d, string key)
        {
            object v;
            if (d == null || !d.TryGetValue(key, out v) || v == null) return "";
            return Convert.ToString(v) ?? "";
        }

        public static int GetInt(Dictionary<string, object> d, string key, int def)
        {
            object v;
            if (d == null || !d.TryGetValue(key, out v) || v == null) return def;
            try { return Convert.ToInt32(v); }
            catch { return def; }
        }

        public static string Serialize(object obj)
        {
            var sb = new StringBuilder();
            Write(sb, obj);
            return sb.ToString();
        }

        static void Write(StringBuilder sb, object obj)
        {
            if (obj == null) { sb.Append("null"); return; }
            if (obj is bool)
            {
                sb.Append(((bool)obj) ? "true" : "false");
                return;
            }
            if (obj is int || obj is long || obj is double || obj is float)
            {
                sb.Append(Convert.ToString(obj, System.Globalization.CultureInfo.InvariantCulture));
                return;
            }
            if (obj is string)
            {
                WriteString(sb, (string)obj);
                return;
            }
            if (obj is Dictionary<string, object>)
            {
                var d = (Dictionary<string, object>)obj;
                sb.Append('{');
                bool first = true;
                foreach (var kv in d)
                {
                    if (!first) sb.Append(',');
                    first = false;
                    WriteString(sb, kv.Key);
                    sb.Append(':');
                    Write(sb, kv.Value);
                }
                sb.Append('}');
                return;
            }
            if (obj is System.Collections.IList)
            {
                var list = (System.Collections.IList)obj;
                sb.Append('[');
                for (int i = 0; i < list.Count; i++)
                {
                    if (i > 0) sb.Append(',');
                    Write(sb, list[i]);
                }
                sb.Append(']');
                return;
            }
            WriteString(sb, Convert.ToString(obj) ?? "");
        }

        static void WriteString(StringBuilder sb, string s)
        {
            sb.Append('"');
            if (s != null)
            {
                foreach (char c in s)
                {
                    switch (c)
                    {
                        case '"': sb.Append("\\\""); break;
                        case '\\': sb.Append("\\\\"); break;
                        case '\n': sb.Append("\\n"); break;
                        case '\r': sb.Append("\\r"); break;
                        case '\t': sb.Append("\\t"); break;
                        default:
                            if (c < 0x20) sb.AppendFormat("\\u{0:x4}", (int)c);
                            else sb.Append(c);
                            break;
                    }
                }
            }
            sb.Append('"');
        }

        static void SkipWs(string s, ref int i)
        {
            while (i < s.Length && char.IsWhiteSpace(s[i])) i++;
        }

        static string ReadString(string s, ref int i)
        {
            SkipWs(s, ref i);
            if (i >= s.Length || s[i] != '"') return null;
            i++;
            var sb = new StringBuilder();
            while (i < s.Length)
            {
                char c = s[i++];
                if (c == '"') break;
                if (c == '\\' && i < s.Length)
                {
                    char n = s[i++];
                    switch (n)
                    {
                        case '"': sb.Append('"'); break;
                        case '\\': sb.Append('\\'); break;
                        case 'n': sb.Append('\n'); break;
                        case 'r': sb.Append('\r'); break;
                        case 't': sb.Append('\t'); break;
                        case 'u':
                            if (i + 4 <= s.Length)
                            {
                                int code;
                                if (int.TryParse(s.Substring(i, 4), System.Globalization.NumberStyles.HexNumber, null, out code))
                                    sb.Append((char)code);
                                i += 4;
                            }
                            break;
                        default: sb.Append(n); break;
                    }
                }
                else sb.Append(c);
            }
            return sb.ToString();
        }

        static object ReadValue(string s, ref int i)
        {
            SkipWs(s, ref i);
            if (i >= s.Length) return null;
            char c = s[i];
            if (c == '"') return ReadString(s, ref i);
            if (c == '{')
            {
                // nested object — scan balanced braces as raw then recurse
                int start = i;
                int depth = 0;
                for (; i < s.Length; i++)
                {
                    if (s[i] == '{') depth++;
                    else if (s[i] == '}')
                    {
                        depth--;
                        if (depth == 0) { i++; break; }
                    }
                    else if (s[i] == '"')
                    {
                        i++;
                        while (i < s.Length)
                        {
                            if (s[i] == '\\') { i += 2; continue; }
                            if (s[i] == '"') break;
                            i++;
                        }
                    }
                }
                return ParseObject(s.Substring(start, i - start));
            }
            if (c == 't' && s.Substring(i).StartsWith("true")) { i += 4; return true; }
            if (c == 'f' && s.Substring(i).StartsWith("false")) { i += 5; return false; }
            if (c == 'n' && s.Substring(i).StartsWith("null")) { i += 4; return null; }
            // number
            int j = i;
            if (s[j] == '-') j++;
            while (j < s.Length && (char.IsDigit(s[j]) || s[j] == '.')) j++;
            string num = s.Substring(i, j - i);
            i = j;
            int iv;
            if (int.TryParse(num, out iv)) return iv;
            double dv;
            if (double.TryParse(num, System.Globalization.NumberStyles.Float, System.Globalization.CultureInfo.InvariantCulture, out dv)) return dv;
            return num;
        }
    }
}
