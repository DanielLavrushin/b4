package log

import (
	"bytes"
	"io"
	"strings"
	"sync"
)

func ParseLevel(s string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "0", "error", "err", "silent":
		return LevelError, true
	case "1", "info":
		return LevelInfo, true
	case "2", "trace":
		return LevelTrace, true
	case "3", "debug":
		return LevelDebug, true
	}
	return 0, false
}

type levelCapWriter struct {
	w    io.Writer
	max  Level
	mu   sync.Mutex
	buf  []byte
	drop bool
}

func CapLevel(w io.Writer, max Level) io.Writer {
	return &levelCapWriter{w: w, max: max}
}

var levelTags = []struct {
	tag   []byte
	level Level
}{
	{[]byte("[INFO]"), LevelInfo},
	{[]byte("[TRACE]"), LevelTrace},
	{[]byte("[DEBUG]"), LevelDebug},
	{[]byte("[ERROR]"), LevelError},
	{[]byte("[WARN]"), LevelError},
}

func lineLevel(line []byte) (Level, bool) {
	head := line
	if len(head) > 48 {
		head = head[:48]
	}
	i := bytes.IndexByte(head, '[')
	if i < 0 {
		return 0, false
	}
	for _, t := range levelTags {
		if bytes.HasPrefix(line[i:], t.tag) {
			return t.level, true
		}
	}
	return 0, false
}

func (c *levelCapWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf = append(c.buf, p...)
	var keep []byte
	start := 0
	for {
		i := bytes.IndexByte(c.buf[start:], '\n')
		if i < 0 {
			break
		}
		end := start + i + 1
		line := c.buf[start:end]
		if lvl, ok := lineLevel(line); ok {
			c.drop = lvl > c.max
		}
		if !c.drop {
			keep = append(keep, line...)
		}
		start = end
	}
	c.buf = append([]byte{}, c.buf[start:]...)
	if len(keep) > 0 {
		if _, err := c.w.Write(keep); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}
