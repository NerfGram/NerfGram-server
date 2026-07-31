// Command adaptiveemoji marks emoji sticker seed packs as adaptive (text_color)
// so clients tint custom emoji to match message text color.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func main() {
	seedRoot := flag.String("seed", "data/sticker-seed/telegram_emoji_export", "emoji export root")
	shortName := flag.String("short-name", "", "optional short_name override for result.set")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: adaptiveemoji [-seed dir] [-short-name name] <set_dir_name> [set_dir_name...]")
		os.Exit(2)
	}
	for _, name := range flag.Args() {
		if err := patch(filepath.Join(*seedRoot, name), *shortName); err != nil {
			fmt.Fprintf(os.Stderr, "patch %s: %v\n", name, err)
			os.Exit(1)
		}
		fmt.Printf("patched adaptive emoji pack %s\n", name)
	}
}

func patch(setDir, shortName string) error {
	path := filepath.Join(setDir, "set_info.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var root map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return fmt.Errorf("parse set_info.json: %w", err)
	}
	result, ok := root["result"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing result object")
	}
	set, ok := result["set"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing result.set object")
	}
	set["text_color"] = true
	if shortName != "" {
		set["short_name"] = shortName
	}
	switch hash := set["hash"].(type) {
	case json.Number:
		if n, err := hash.Int64(); err == nil {
			set["hash"] = json.Number(strconv.FormatInt(n+1, 10))
		}
	case float64:
		set["hash"] = json.Number(strconv.FormatInt(int64(hash)+1, 10))
	}
	docs, ok := result["documents"].([]any)
	if !ok {
		return fmt.Errorf("missing result.documents array")
	}
	for _, item := range docs {
		doc, ok := item.(map[string]any)
		if !ok {
			continue
		}
		attrs, ok := doc["attributes"].([]any)
		if !ok {
			continue
		}
		for _, attrItem := range attrs {
			attr, ok := attrItem.(map[string]any)
			if !ok {
				continue
			}
			if attr["_"] != "DocumentAttributeCustomEmoji" {
				continue
			}
			attr["text_color"] = true
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(root); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
