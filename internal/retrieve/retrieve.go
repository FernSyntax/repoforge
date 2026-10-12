package retrieve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Defaults bound every build so indexing cannot run away.
const (
	defaultChunkLines = 50
	defaultMaxFiles   = 10000
	defaultMaxBytes   = 256 << 10
	defaultTopN       = 5
	maxTopN           = 50
)

var (
	defaultExts     = []string{".md", ".markdown", ".go", ".yaml", ".yml", ".json", ".txt", ".mod", ".sum", ".sh", ".toml"}
	defaultSkipDirs = []string{".git", "node_modules", "vendor", ".cache", "dist", "build", ".idea", ".vscode"}
	// defaultSkipFiles names secret-bearing and artifact files. These
	// never enter the index under any extension rule.
	defaultSkipFiles = []string{".env"}
	defaultSkipExts  = []string{".pem", ".key", ".token", ".prof", ".png", ".jpg", ".bin"}
)

// Options configures a build. Zero values select the defaults.
type Options struct {
	IncludeExts  []string
	SkipDirs     []string
	MaxFileBytes int64
	MaxFiles     int
	ChunkLines   int
	// Revision stamps provenance (for example a git SHA). It is
	// caller-supplied metadata, never verified here.
	Revision string
}

func (o Options) withDefaults() Options {
	if o.IncludeExts == nil {
		o.IncludeExts = defaultExts
	}
	if o.SkipDirs == nil {
		o.SkipDirs = defaultSkipDirs
	}
	if o.MaxFileBytes <= 0 {
		o.MaxFileBytes = defaultMaxBytes
	}
	if o.MaxFiles <= 0 {
		o.MaxFiles = defaultMaxFiles
	}
	if o.ChunkLines <= 0 {
		o.ChunkLines = defaultChunkLines
	}
	return o
}

// Chunk is one retrievable unit with full provenance.
type Chunk struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Index     int    `json:"index"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Text      string `json:"text"`
	Hash      string `json:"hash"`
}

// Doc is one indexed file.
type Doc struct {
	Path   string  `json:"path"`
	Hash   string  `json:"hash"`
	Chunks []Chunk `json:"chunks"`
}

// Index is the built knowledge base. BuiltAt is operational metadata;
// Docs determine equality.
type Index struct {
	Revision string    `json:"revision"`
	BuiltAt  time.Time `json:"builtAt"`
	Docs     []Doc     `json:"docs"`

	flat []*Chunk
}

// Hit is one ranked result: inert content with its source attached.
type Hit struct {
	Chunk Chunk
	Score int
}

// Build walks root and indexes every admissible file. The walk is
// lexical, chunking is positional, and scoring inputs are content-only,
// so identical trees always build identical indexes.
func Build(root string, opts Options) (*Index, error) {
	opts = opts.withDefaults()
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("retrieve: reading %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("retrieve: %q is not a directory", root)
	}
	x := &Index{Revision: opts.Revision, BuiltAt: time.Now().UTC()}
	count := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && contains(opts.SkipDirs, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !admissible(d.Name(), opts) {
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		if st.Size() > opts.MaxFileBytes {
			return nil
		}
		count++
		if count > opts.MaxFiles {
			return fmt.Errorf("retrieve: file limit %d exceeded", opts.MaxFiles)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		x.Docs = append(x.Docs, chunkFile(filepath.ToSlash(rel), raw, opts.ChunkLines))
		return nil
	})
	if err != nil {
		return nil, err
	}
	x.reindex()
	return x, nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// admissible applies the exclusion-first walk rules: secrets and
// artifacts are out by name regardless of extension, then the
// extension allowlist decides.
func admissible(name string, opts Options) bool {
	if name == ".env" || strings.HasPrefix(name, ".env.") {
		return false
	}
	for _, skip := range defaultSkipFiles {
		if name == skip {
			return false
		}
	}
	ext := strings.ToLower(filepath.Ext(name))
	for _, skip := range defaultSkipExts {
		if ext == skip {
			return false
		}
	}
	if strings.HasPrefix(name, "id_rsa") {
		return false
	}
	for _, allow := range opts.IncludeExts {
		if ext == allow {
			return true
		}
	}
	return false
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// chunkFile splits content into positional line windows. Chunk n
// covers lines [n*size+1, min((n+1)*size, total)]: stable across runs
// for identical bytes.
func chunkFile(path string, raw []byte, size int) Doc {
	fileHash := hashOf(raw)
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	doc := Doc{Path: path, Hash: fileHash}
	for start := 0; start < len(lines); start += size {
		end := start + size
		if end > len(lines) {
			end = len(lines)
		}
		body := strings.Join(lines[start:end], "\n")
		n := len(doc.Chunks)
		doc.Chunks = append(doc.Chunks, Chunk{
			ID:        fmt.Sprintf("%s#%d", path, n),
			Path:      path,
			Index:     n,
			StartLine: start + 1,
			EndLine:   end,
			Text:      body,
			Hash:      hashOf([]byte(body)),
		})
	}
	return doc
}

func (x *Index) reindex() {
	x.flat = nil
	for i := range x.Docs {
		for j := range x.Docs[i].Chunks {
			x.flat = append(x.flat, &x.Docs[i].Chunks[j])
		}
	}
}

// Search ranks chunks for the query: token frequency, triple weight
// for filename matches, and an exact-phrase bonus. Ordering is total
// (score, path, chunk index), so rankings are byte-stable for
// identical indexes. topN <= 0 selects the default.
func (x *Index) Search(query string, topN int) []Hit {
	if topN <= 0 {
		topN = defaultTopN
	}
	if topN > maxTopN {
		topN = maxTopN
	}
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil
	}
	phrase := strings.ToLower(strings.TrimSpace(query))
	var hits []Hit
	for _, c := range x.flat {
		score := scoreChunk(c, tokens, phrase)
		if score > 0 {
			hits = append(hits, Hit{Chunk: *c, Score: score})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if hits[i].Chunk.Path != hits[j].Chunk.Path {
			return hits[i].Chunk.Path < hits[j].Chunk.Path
		}
		return hits[i].Chunk.Index < hits[j].Chunk.Index
	})
	if len(hits) > topN {
		hits = hits[:topN]
	}
	return hits
}

func tokenize(s string) []string {
	lower := strings.ToLower(s)
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	out := fields[:0]
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func scoreChunk(c *Chunk, tokens []string, phrase string) int {
	body := strings.ToLower(c.Text)
	name := strings.ToLower(c.Path)
	score := 0
	for _, tok := range tokens {
		score += strings.Count(body, tok)
		score += 3 * strings.Count(name, tok)
	}
	if phrase != "" && strings.Contains(body, phrase) {
		score += 5
	}
	return score
}

// Refresh rebuilds from the current tree and reports what moved:
// added, changed, and removed paths. Deleted or superseded documents
// never remain silently authoritative: they are listed as removed and
// absent from the new index.
func (x *Index) Refresh(root string, opts Options) (added, changed, removed []string, next *Index, err error) {
	if opts.Revision == "" {
		opts.Revision = x.Revision
	}
	fresh, err := Build(root, opts)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	old := map[string]string{}
	for _, d := range x.Docs {
		old[d.Path] = d.Hash
	}
	seen := map[string]bool{}
	for _, d := range fresh.Docs {
		seen[d.Path] = true
		if h, ok := old[d.Path]; !ok {
			added = append(added, d.Path)
		} else if h != d.Hash {
			changed = append(changed, d.Path)
		}
	}
	for _, d := range x.Docs {
		if !seen[d.Path] {
			removed = append(removed, d.Path)
		}
	}
	sort.Strings(added)
	sort.Strings(changed)
	sort.Strings(removed)
	return added, changed, removed, fresh, nil
}

// Save persists the index atomically (temp file plus rename).
func (x *Index) Save(path string) error {
	raw, err := json.MarshalIndent(x, "", "  ")
	if err != nil {
		return fmt.Errorf("retrieve: encoding index: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "index-*.tmp")
	if err != nil {
		return fmt.Errorf("retrieve: index temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("retrieve: writing index: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("retrieve: writing index: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("retrieve: publishing index: %w", err)
	}
	return nil
}

// LoadIndex reads a saved index and rebuilds its search structures.
func LoadIndex(path string) (*Index, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("retrieve: reading index %q: %w", path, err)
	}
	x := &Index{}
	if err := json.Unmarshal(raw, x); err != nil {
		return nil, fmt.Errorf("retrieve: decoding index %q: %w", path, err)
	}
	x.reindex()
	return x, nil
}
