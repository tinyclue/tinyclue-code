package tools

import (
	"strings"
	"testing"
)

// TestNormalizeForFuzzyMatchWithMap 验证模糊归一化结果与旧实现逐字节一致，且偏移映射
// 边界正确、单调不减；未替换的 ASCII 区域应逐字节映射到原始文本。
func TestNormalizeForFuzzyMatchWithMap(t *testing.T) {
	content := "a “quote”— dash \u00A0 and trailing  \nnext  \n"
	normalized, m := normalizeForFuzzyMatchWithMap(content)

	if got, want := normalized, normalizeForFuzzyMatch(content); got != want {
		t.Fatalf("normalizeForFuzzyMatchWithMap output diverges:\n got  %q\n want %q", got, want)
	}
	if m[0] != 0 {
		t.Fatalf("m[0] = %d, want 0", m[0])
	}
	if m[len(normalized)] != len(content) {
		t.Fatalf("m[len(normalized)] = %d, want %d", m[len(normalized)], len(content))
	}
	for i := 1; i < len(normalized); i++ {
		if m[i] < m[i-1] {
			t.Fatalf("m not monotonic at %d: %d < %d", i, m[i], m[i-1])
		}
	}

	// 未受归一化影响的 ASCII 片段应逐字节映射回原文。
	asciiIdx := strings.Index(content, "next")
	ni := strings.Index(normalized, "next")
	if asciiIdx == -1 || ni == -1 {
		t.Fatal("fixture missing ascii region")
	}
	for k := 0; k < len("next"); k++ {
		if m[ni+k] != asciiIdx+k {
			t.Fatalf("ascii mapping off at %d: got %d want %d", k, m[ni+k], asciiIdx+k)
		}
	}
}

// TestFuzzyEditPreservesUntouchedRegions 验证模糊匹配编辑时仅命中区间被替换，文件其余
// 部分（含 em 破折号、行尾空白等会被归一化改写的字节）保持字节原样——不再整文件有损重写。
func TestFuzzyEditPreservesUntouchedRegions(t *testing.T) {
	content := "package main\n\nfunc main() {\n" +
		"    fmt.Println(“hello”)\n" +
		"    const dash = \"em — dash\"\n" +
		"    stray   \n" +
		"}\n"
	edits := []editOp{{OldText: `fmt.Println("hello")`, NewText: `fmt.Println("hi")`}}

	result, err := applyEditsToNormalizedContent(content, edits, "test.go")
	if err != nil {
		t.Fatalf("applyEdits: %v", err)
	}

	if !strings.Contains(result.newContent, `fmt.Println("hi")`) {
		t.Errorf("edit not applied; newContent:\n%s", result.newContent)
	}
	if !strings.Contains(result.newContent, `const dash = "em — dash"`) {
		t.Errorf("untouched em-dash line altered:\n%s", result.newContent)
	}
	if !strings.Contains(result.newContent, "    stray   \n") {
		t.Errorf("untouched trailing-whitespace line altered:\n%s", result.newContent)
	}
	if strings.Contains(result.newContent, "“") {
		t.Errorf("matched curly-quote region should have been replaced wholesale:\n%s", result.newContent)
	}
}

// TestExactEditPreservesUntouchedRegions 精确匹配路径（anyFuzzy==false）不回归：
// 未触碰行仍保持原样。
func TestExactEditPreservesUntouchedRegions(t *testing.T) {
	content := "a — b\nc   \n"
	edits := []editOp{{OldText: "a — b", NewText: "A - B"}}

	result, err := applyEditsToNormalizedContent(content, edits, "test.go")
	if err != nil {
		t.Fatalf("applyEdits: %v", err)
	}
	want := "A - B\nc   \n"
	if result.newContent != want {
		t.Errorf("newContent = %q, want %q", result.newContent, want)
	}
}

// TestFuzzyEditNFKCComposition 验证 NFKC 组合：原文是分解式 e + 组合重音（4 字节），
// oldText 是预组合 é，命中后整个组合序列应被替换，而不是只替换片段。
func TestFuzzyEditNFKCComposition(t *testing.T) {
	content := "prefix e\u0301 suffix\n"
	edits := []editOp{{OldText: "é", NewText: "E"}}

	result, err := applyEditsToNormalizedContent(content, edits, "test.go")
	if err != nil {
		t.Fatalf("applyEdits: %v", err)
	}
	want := "prefix E suffix\n"
	if result.newContent != want {
		t.Errorf("newContent = %q, want %q", result.newContent, want)
	}
}
