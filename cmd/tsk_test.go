package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractMetaTitle(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    string
	}{
		{
			name: "h1 wins over later h2",
			body: "---\nsummary: ignored\n---\n\n# Real Title\n\n## Status\n",
			want: "Real Title",
		},
		{
			name: "summary wins over h2 when no h1",
			body: "---\nsummary: From Frontmatter\n---\n\n## Status\n\nbody.\n",
			want: "From Frontmatter",
		},
		{
			name: "double-quoted summary loses its quotes",
			body: "---\nsummary: \"one-liner: with a colon\"\n---\n\n## Status\n",
			want: "one-liner: with a colon",
		},
		{
			name: "single-quoted summary loses its quotes",
			body: "---\nsummary: 'it''s quoted'\n---\n\n## Status\n",
			want: "it's quoted",
		},
		{
			name: "h2 fallback when no h1 and no summary",
			body: "---\n---\n\n## Legacy H2 Title\n\n### sub\n",
			want: "Legacy H2 Title",
		},
		{
			name: "filename stem when nothing else",
			body: "---\n---\n\nplain prose, no headings.\n",
			want: "no-headings",
		},
		{
			name: "skips heading inside fenced code block",
			body: "---\n---\n\n## Real H2\n\n```bash\n# Download the schema\ncurl ...\n```\n\n# Real H1\n",
			want: "Real H1",
		},
		{
			name: "shell comments in code don't get picked as h1",
			body: "---\n---\n\n```\n# comment\n```\n\n## section\n",
			want: "section",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "no-headings.md")
			if err := os.WriteFile(p, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			meta := extractMeta(p)
			if meta.Title != tc.want {
				t.Errorf("title = %q, want %q", meta.Title, tc.want)
			}
		})
	}
}
