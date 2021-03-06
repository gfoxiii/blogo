package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/hoisie/mustache"
	"github.com/hoisie/web"
	"code.google.com/p/go.net/html"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Tag struct {
	Name string
}

type Entry struct {
	Id       string
	Filename string
	Title    string
	Body     string
	Created  time.Time
	Category string
	Author   string
	Tags     []Tag
}

func toTextChild(w io.Writer, n *html.Node) error {
	switch n.Type {
	case html.ErrorNode:
		return errors.New("unexpected ErrorNode")
	case html.DocumentNode:
		return errors.New("unexpected DocumentNode")
	case html.ElementNode:
	case html.TextNode:
		w.Write([]byte(n.Data))
	case html.CommentNode:
		return errors.New("COMMENT")
	default:
		return errors.New("unknown node type")
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := toTextChild(w, c); err != nil {
			return err
		}
	}
	return nil
}

func toText(n *html.Node) (string, error) {
	if n == nil || n.FirstChild == nil {
		return "", nil
	}
	b := bytes.NewBuffer(nil)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := toTextChild(b, c); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

func GetEntry(filename string) (entry *Entry, err error) {
	fi, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	in_body := false
	re, err := regexp.Compile("^meta-([a-zA-Z]+):[:space:]*(.*)$")
	if err != nil {
		return nil, err
	}
	for n, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if n == 0 {
			entry = new(Entry)
			entry.Title = line
			entry.Filename = filepath.Clean(filename)
			entry.Tags = []Tag{}
			entry.Created = fi.ModTime()
			continue
		}
		if n > 0 && len(line) == 0 {
			in_body = true
			continue
		}
		if in_body == false && re.MatchString(line) {
			submatch := re.FindStringSubmatch(line)
			if submatch[1] == "tags" {
				tags := strings.Split(submatch[2], ",")
				entry.Tags = make([]Tag, len(tags))
				for i, t := range tags {
					entry.Tags[i].Name = strings.TrimSpace(t)
				}
			}
			if submatch[1] == "author" {
				entry.Author = submatch[2]
			}
		} else {
			entry.Body += strings.Trim(line, "\r") + "\n"
		}
	}
	if entry == nil {
		err = errors.New("invalid entry file")
	}
	return
}

func GetEntries(root string, useSummary bool) (entries []*Entry, err error) {
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if strings.ToLower(filepath.Ext(path)) != ".txt" {
			return nil
		}
		entry, _ := GetEntry(path)
		if entry == nil {
			return nil
		}
		entries = append(entries, entry)
		if useSummary {
			doc, err := html.Parse(strings.NewReader(entry.Body))
			if err == nil {
				if text, err := toText(doc); err == nil {
					if len(text) > 500 {
						text = text[0:500] + "..."
					}
					entry.Body = text
				}
			}
		}
		entry.Id = entry.Filename[len(root):len(entry.Filename)-3] + "html"
		return nil
	})
	return
}

type Config map[string]interface{}

func (c *Config) Set(key string, val interface{}) {
	(*c)[key] = val
}

func (c *Config) Is(key string) bool {
	val, ok := (*c)[key].(bool)
	if !ok {
		return false
	}
	return val
}

func (c *Config) Get(key string) string {
	val, ok := (*c)[key].(string)
	if !ok {
		return ""
	}
	return val
}

func LoadConfig() (config Config) {
	root, _ := filepath.Split(filepath.Clean(os.Args[0]))
	b, err := ioutil.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		log.Fatal(err)
	}
	json.Unmarshal(b, &config)
	return
}

func Render(ctx *web.Context, tmpl string, config *Config, name string, data interface{}) {
	tmpl = filepath.Join(config.Get("datadir"), tmpl)
	ctx.WriteString(mustache.RenderFile(tmpl,
		map[string]interface{}{
			"config": config,
			name:     data}))
}

func main() {
	config := LoadConfig()
	web.Get("/(.*)", func(ctx *web.Context, path string) {
		config = LoadConfig()
		datadir := config.Get("datadir")
		if path == "" || path[len(path)-1] == '/' {
			dir := filepath.Join(datadir, path)
			stat, err := os.Stat(dir)
			if err != nil || !stat.IsDir() {
				ctx.NotFound("File Not Found")
				return
			}
			entries, err := GetEntries(dir, config.Is("useSummary"))
			if err == nil {
				Render(ctx, "entries.mustache", &config, "entries", entries)
				return
			}
		} else if len(path) > 5 && path[len(path)-5:] == ".html" {
			file := filepath.Join(datadir, path[:len(path)-5]+".txt")
			_, err := os.Stat(file)
			if err != nil {
				ctx.NotFound("File Not Found" + err.Error())
				return
			}
			entry, err := GetEntry(file)
			if err == nil {
				entry.Id = entry.Filename[len(datadir):len(entry.Filename)-3] + "html"
				Render(ctx, "entry.mustache", &config, "entry", entry)
				return
			}
		} else if path == "index.rss" {
			entries, err := GetEntries(datadir, config.Is("useSummary"))
			if err == nil {
				ctx.SetHeader("Content-Type", "application/rss+xml; charset=utf-8", true)
				Render(ctx, "entries.rss", &config, "entries", entries)
				return
			}
		}
		ctx.Abort(500, "Server Error")
	})
	//web.Config.RecoverPanic = false
	web.Config.StaticDir = config.Get("staticdir")
	web.Run(config.Get("host"))
}
