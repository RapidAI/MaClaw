package agent

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// extractGLTFText reads a GLB or glTF JSON document as a scene inventory.
// Mesh buffers, textures, and images are not decoded.
func extractGLTFText(filePath string) (text string, err error) {
	defer recoverLegacyOfficeExtraction("GLB", &text, &err)
	if err := preflightDirectDocumentInput(filePath); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	meta, err := gltfMetadataJSON(data)
	if err != nil {
		return "", err
	}
	var doc gltfDocument
	if err := json.Unmarshal(meta, &doc); err != nil {
		return "", fmt.Errorf("无法解析三维模型: %v", err)
	}
	if doc.Asset.Version != "2.0" {
		return "", fmt.Errorf("仅支持 glTF 2.0 模型")
	}
	if err := doc.validate(); err != nil {
		return "", err
	}
	out := doc.render()
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("模型里没有可读取的结构")
	}
	return out, nil
}

func trimGLTFJSON(data []byte) []byte {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	return bytes.Trim(data, " \r\n\t\x00")
}

func gltfMetadataJSON(data []byte) ([]byte, error) {
	trimmed := trimGLTFJSON(data)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return trimmed, nil
	}
	if len(data) < 20 || string(data[:4]) != "glTF" || binary.LittleEndian.Uint32(data[4:8]) != 2 {
		return nil, fmt.Errorf("无效的 GLB 文件")
	}
	if binary.LittleEndian.Uint32(data[8:12]) != uint32(len(data)) || len(data)%4 != 0 {
		return nil, fmt.Errorf("GLB 容器长度不匹配")
	}
	offset := 12
	for index := 0; offset+8 <= len(data); index++ {
		length := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		kind := binary.LittleEndian.Uint32(data[offset+4 : offset+8])
		offset += 8
		if length < 0 || length%4 != 0 || offset+length > len(data) {
			return nil, fmt.Errorf("GLB 块长度无效")
		}
		chunk := data[offset : offset+length]
		offset += length
		if index == 0 && kind != 0x4E4F534A {
			return nil, fmt.Errorf("GLB 首块必须为 JSON")
		}
		if kind == 0x4E4F534A {
			if index != 0 || length > 16<<20 {
				return nil, fmt.Errorf("GLB JSON 块无效")
			}
			return trimGLTFJSON(chunk), nil
		}
	}
	return nil, fmt.Errorf("GLB 里没有 JSON 元数据")
}

type gltfDocument struct {
	Asset struct {
		Version   string `json:"version"`
		Generator string `json:"generator"`
		Copyright string `json:"copyright"`
	} `json:"asset"`
	Scenes    []gltfScene    `json:"scenes"`
	Nodes     []gltfNode     `json:"nodes"`
	Meshes    []gltfMesh     `json:"meshes"`
	Materials []gltfMaterial `json:"materials"`
}

type gltfScene struct {
	Name  string `json:"name"`
	Nodes []int  `json:"nodes"`
}

type gltfNode struct {
	Name     string          `json:"name"`
	Mesh     *int            `json:"mesh"`
	Children []int           `json:"children"`
	Extras   json.RawMessage `json:"extras"`
}

type gltfMesh struct {
	Name       string `json:"name"`
	Primitives []struct {
		Material *int `json:"material"`
	} `json:"primitives"`
	Extras json.RawMessage `json:"extras"`
}

type gltfMaterial struct {
	Name   string          `json:"name"`
	Extras json.RawMessage `json:"extras"`
}

func (d *gltfDocument) validate() error {
	if len(d.Nodes) > 10000 {
		return fmt.Errorf("模型节点数量超过上限")
	}
	parents := make([]int, len(d.Nodes))
	for i := range parents {
		parents[i] = -1
	}
	for i, node := range d.Nodes {
		if node.Mesh != nil && (*node.Mesh < 0 || *node.Mesh >= len(d.Meshes)) {
			return fmt.Errorf("节点[%d] 的网格引用无效", i)
		}
		for _, child := range node.Children {
			if child < 0 || child >= len(d.Nodes) || parents[child] != -1 {
				return fmt.Errorf("节点[%d] 的子节点引用无效", i)
			}
			parents[child] = i
		}
	}
	seen := 0
	var walk func(int)
	walk = func(i int) {
		seen++
		if seen > len(d.Nodes) {
			return
		}
		for _, child := range d.Nodes[i].Children {
			walk(child)
		}
	}
	for i, parent := range parents {
		if parent == -1 {
			walk(i)
		}
	}
	if seen != len(d.Nodes) {
		return fmt.Errorf("模型节点存在循环引用")
	}
	return nil
}

func (d *gltfDocument) render() string {
	var b strings.Builder
	b.WriteString("# 三维模型\n\n")
	fmt.Fprintf(&b, "格式：glTF %s；场景：%d；节点：%d；网格：%d；材质：%d\n", d.Asset.Version, len(d.Scenes), len(d.Nodes), len(d.Meshes), len(d.Materials))
	if d.Asset.Generator != "" {
		b.WriteString("生成工具：")
		b.WriteString(d.Asset.Generator)
		b.WriteByte('\n')
	}
	if d.Asset.Copyright != "" {
		b.WriteString("版权：")
		b.WriteString(d.Asset.Copyright)
		b.WriteByte('\n')
	}
	b.WriteString("本结果只列出场景、节点、网格和已有属性，不包含几何和贴图。\n")
	for i, scene := range d.Scenes {
		fmt.Fprintf(&b, "\n## 场景[%d] %s\n", i, scene.Name)
		b.WriteString("根节点：")
		b.WriteString(d.nodeList(scene.Nodes))
		b.WriteByte('\n')
	}
	for i, node := range d.Nodes {
		fmt.Fprintf(&b, "\n## 节点[%d] %s\n", i, node.Name)
		if node.Mesh != nil && *node.Mesh >= 0 && *node.Mesh < len(d.Meshes) {
			fmt.Fprintf(&b, "引用网格：网格[%d] %s\n", *node.Mesh, d.Meshes[*node.Mesh].Name)
		}
		if len(node.Children) > 0 {
			b.WriteString("子节点：")
			b.WriteString(d.nodeList(node.Children))
			b.WriteByte('\n')
		}
		writeGLTFExtras(&b, node.Extras)
	}
	for i, mesh := range d.Meshes {
		fmt.Fprintf(&b, "\n## 网格[%d] %s\n", i, mesh.Name)
		fmt.Fprintf(&b, "图元数量：%d\n", len(mesh.Primitives))
		writeGLTFExtras(&b, mesh.Extras)
	}
	for i, material := range d.Materials {
		if material.Name == "" && len(material.Extras) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## 材质[%d] %s\n", i, material.Name)
		writeGLTFExtras(&b, material.Extras)
	}
	if len(d.Scenes) == 0 && len(d.Nodes) == 0 && len(d.Meshes) == 0 {
		return ""
	}
	return strings.TrimSpace(b.String())
}

func (d *gltfDocument) nodeList(indexes []int) string {
	if len(indexes) == 0 {
		return "（无）"
	}
	parts := make([]string, 0, len(indexes))
	for _, index := range indexes {
		if index < 0 || index >= len(d.Nodes) {
			continue
		}
		parts = append(parts, fmt.Sprintf("节点[%d] %s", index, d.Nodes[index].Name))
	}
	if len(parts) == 0 {
		return "（无）"
	}
	return strings.Join(parts, "、")
}

func writeGLTFExtras(b *strings.Builder, raw json.RawMessage) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		if len(raw) <= 200 {
			b.WriteString("属性：")
			b.Write(raw)
			b.WriteByte('\n')
		}
		return
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := bytes.TrimSpace(fields[key])
		if len(value) == 0 || len(value) > 200 {
			continue
		}
		b.WriteString("属性：")
		b.WriteString(key)
		b.WriteString(" = ")
		b.Write(value)
		b.WriteByte('\n')
	}
}
