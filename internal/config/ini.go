package config

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
)

// iniDoc is a minimal, dependency-free INI reader/writer. It intentionally
// tolerates the comment style used by the Python version's configparser
// (comment_prefixes='/') in addition to the more usual '#'/';' so an
// existing ~/.autoortho from the Python release can be read as-is.
type iniDoc struct {
	order    []string
	sections map[string]*iniSection
}

type iniSection struct {
	order  []string
	values map[string]string
}

func newIniDoc() *iniDoc {
	return &iniDoc{sections: map[string]*iniSection{}}
}

func (d *iniDoc) section(name string) *iniSection {
	s, ok := d.sections[name]
	if !ok {
		s = &iniSection{values: map[string]string{}}
		d.sections[name] = s
		d.order = append(d.order, name)
	}
	return s
}

func (s *iniSection) get(key string) (string, bool) {
	v, ok := s.values[key]
	return v, ok
}

func (s *iniSection) set(key, value string) {
	if _, ok := s.values[key]; !ok {
		s.order = append(s.order, key)
	}
	s.values[key] = value
}

func parseIni(r io.Reader) (*iniDoc, error) {
	doc := newIniDoc()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var cur *iniSection
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "/") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(line[1 : len(line)-1])
			cur = doc.section(name)
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 || cur == nil {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		cur.set(key, val)
	}
	return doc, scanner.Err()
}

func writeIni(w io.Writer, doc *iniDoc) error {
	sections := append([]string(nil), doc.order...)
	sort.SliceStable(sections, func(i, j int) bool { return false }) // preserve insertion order
	for _, name := range sections {
		if _, err := fmt.Fprintf(w, "[%s]\n", name); err != nil {
			return err
		}
		sec := doc.sections[name]
		for _, key := range sec.order {
			if _, err := fmt.Fprintf(w, "%s = %s\n", key, sec.values[key]); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}
