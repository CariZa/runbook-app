// Package notion reads a Notion page and turns it into the app's Markdown dialect, so the
// existing paste parser (runbook.ParsePaste) can make steps from it. It only ever reads:
// nothing here writes to Notion.
package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// APIVersion is the Notion-Version header every request must carry.
const APIVersion = "2022-06-28"

// maxChildDepth bounds how far nested blocks (toggles, list items) are followed.
const maxChildDepth = 3

// Page is a fetched Notion page, ready for runbook.ParsePaste.
type Page struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	LastEdited time.Time `json:"lastEdited"`
	Markdown   string    `json:"markdown"`
	// Skipped describes blocks that have no place in a runbook ("2 images", "1 table").
	Skipped []string `json:"skipped"`
}

// Client talks to the Notion API with an integration token.
type Client struct {
	Token   string
	BaseURL string // defaults to https://api.notion.com
	HTTP    *http.Client
}

var idRe = regexp.MustCompile(`[0-9a-fA-F]{32}`)

// PageID pulls the page id out of a Notion URL (or accepts a bare id). Database and view
// links are rejected: a runbook comes from one page.
func PageID(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("paste a Notion page link")
	}
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		if !strings.Contains(u.Host, "notion.so") && !strings.Contains(u.Host, "notion.site") {
			return "", fmt.Errorf("%s is not a Notion link", u.Host)
		}
		if u.Query().Get("v") != "" {
			return "", fmt.Errorf("that's a database view; open the page itself and copy its link")
		}
		s = u.Path
		if frag := u.Query().Get("p"); frag != "" { // ...?p=<pageid> on database rows
			s = frag
		}
	}
	all := idRe.FindAllString(strings.ReplaceAll(s, "-", ""), -1)
	if len(all) == 0 {
		return "", fmt.Errorf("no page id in that link")
	}
	return dashed(all[len(all)-1]), nil
}

func dashed(id string) string {
	id = strings.ToLower(id)
	return fmt.Sprintf("%s-%s-%s-%s-%s", id[0:8], id[8:12], id[12:16], id[16:20], id[20:32])
}

// FetchPage reads a page and its blocks and renders them as Markdown.
func (c *Client) FetchPage(ctx context.Context, id string) (Page, error) {
	var meta pageMeta
	if err := c.get(ctx, "/v1/pages/"+id, &meta); err != nil {
		return Page{}, err
	}
	p := Page{ID: id, URL: meta.URL, LastEdited: meta.LastEditedTime, Title: meta.title()}

	skipped := map[string]int{}
	body, err := c.renderChildren(ctx, id, 0, skipped)
	if err != nil {
		return Page{}, err
	}
	md := ""
	if p.Title != "" {
		md = "# " + p.Title + "\n\n"
	}
	p.Markdown = md + body
	p.Skipped = summary(skipped)
	return p, nil
}

func (c *Client) renderChildren(ctx context.Context, parent string, depth int, skipped map[string]int) (string, error) {
	var out strings.Builder
	cursor := ""
	for {
		path := "/v1/blocks/" + parent + "/children?page_size=100"
		if cursor != "" {
			path += "&start_cursor=" + url.QueryEscape(cursor)
		}
		var list blockList
		if err := c.get(ctx, path, &list); err != nil {
			return "", err
		}
		for _, b := range list.Results {
			text, recurse := render(b, skipped)
			out.WriteString(text)
			if recurse && b.HasChildren && depth < maxChildDepth {
				child, err := c.renderChildren(ctx, b.ID, depth+1, skipped)
				if err != nil {
					return "", err
				}
				out.WriteString(child)
			}
		}
		if !list.HasMore || list.NextCursor == "" {
			break
		}
		cursor = list.NextCursor
	}
	return out.String(), nil
}

// render turns one block into Markdown. The second result says whether its children are
// worth following.
func render(b block, skipped map[string]int) (string, bool) {
	switch b.Type {
	case "heading_1":
		return "\n## " + plain(b.Heading1.RichText) + "\n\n", true // a page's top level is a step
	case "heading_2":
		return "\n## " + plain(b.Heading2.RichText) + "\n\n", true
	case "heading_3":
		return "\n### " + plain(b.Heading3.RichText) + "\n\n", true
	case "code":
		lang := b.Code.Language
		if lang == "plain text" || lang == "" {
			lang = "sh"
		}
		body := plain(b.Code.RichText)
		fence := "```"
		for strings.Contains(body, fence) {
			fence += "`"
		}
		return fence + lang + "\n" + body + "\n" + fence + "\n\n", false
	case "paragraph":
		return para(plain(b.Paragraph.RichText)), true
	case "quote":
		return para(plain(b.Quote.RichText)), true
	case "callout":
		return para(plain(b.Callout.RichText)), true
	case "toggle":
		return para(plain(b.Toggle.RichText)), true
	case "bulleted_list_item":
		return para("- " + plain(b.Bulleted.RichText)), true
	case "numbered_list_item":
		return para("1. " + plain(b.Numbered.RichText)), true
	case "to_do":
		box := "[ ]"
		if b.ToDo.Checked {
			box = "[x]"
		}
		return para("- " + box + " " + plain(b.ToDo.RichText)), true
	case "divider":
		return "\n", false
	case "image", "video", "file", "pdf", "embed", "bookmark":
		skipped[b.Type]++
		return "", false
	case "table", "table_row", "column_list", "column", "synced_block", "child_database", "child_page":
		if b.Type != "table_row" && b.Type != "column" {
			skipped[b.Type]++
		}
		return "", false
	default:
		skipped["other block"]++
		return "", false
	}
}

func para(s string) string {
	if strings.TrimSpace(s) == "" {
		return "\n"
	}
	return s + "\n\n"
}

// plain flattens rich text, keeping inline code as backticks.
func plain(rt []richText) string {
	var b strings.Builder
	for _, t := range rt {
		s := t.PlainText
		if t.Annotations.Code && strings.TrimSpace(s) != "" {
			s = "`" + s + "`"
		}
		b.WriteString(s)
	}
	return strings.TrimRight(b.String(), " \t")
}

func summary(counts map[string]int) []string {
	names := map[string]string{
		"image": "image", "video": "video", "file": "file", "pdf": "PDF", "embed": "embed",
		"bookmark": "bookmark", "table": "table", "column_list": "column layout",
		"synced_block": "synced block", "child_database": "database", "child_page": "sub-page",
	}
	var out []string
	for k, n := range counts {
		name := names[k]
		if name == "" {
			name = k
		}
		if n == 1 {
			out = append(out, "1 "+name)
		} else {
			out = append(out, fmt.Sprintf("%d %ss", n, name))
		}
	}
	sort.Strings(out)
	return out
}

// --- HTTP ---

func (c *Client) get(ctx context.Context, path string, into any) error {
	base := c.BaseURL
	if base == "" {
		base = "https://api.notion.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Notion-Version", APIVersion)

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach Notion: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var e apiError
		json.NewDecoder(res.Body).Decode(&e)
		return apiMessage(res.StatusCode, e)
	}
	return json.NewDecoder(res.Body).Decode(into)
}

// apiMessage turns Notion's errors into something worth showing a person.
func apiMessage(status int, e apiError) error {
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("Notion rejected the token: check it, or create a new integration token")
	case http.StatusNotFound:
		return fmt.Errorf("Notion can't see that page: open it, then Share it with your integration")
	case http.StatusTooManyRequests:
		return fmt.Errorf("Notion is rate-limiting; try again in a moment")
	}
	if e.Message != "" {
		return fmt.Errorf("Notion: %s", e.Message)
	}
	return fmt.Errorf("Notion returned %d", status)
}

type apiError struct {
	Message string `json:"message"`
}

type pageMeta struct {
	URL            string          `json:"url"`
	LastEditedTime time.Time       `json:"last_edited_time"`
	Properties     map[string]prop `json:"properties"`
}

// title finds the page's title property, whatever it happens to be called.
func (m pageMeta) title() string {
	for _, p := range m.Properties {
		if p.Type == "title" {
			return plain(p.Title)
		}
	}
	return ""
}

type prop struct {
	Type  string     `json:"type"`
	Title []richText `json:"title"`
}

type blockList struct {
	Results    []block `json:"results"`
	HasMore    bool    `json:"has_more"`
	NextCursor string  `json:"next_cursor"`
}

type textBlock struct {
	RichText []richText `json:"rich_text"`
}

type block struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	HasChildren bool   `json:"has_children"`

	Paragraph textBlock `json:"paragraph"`
	Heading1  textBlock `json:"heading_1"`
	Heading2  textBlock `json:"heading_2"`
	Heading3  textBlock `json:"heading_3"`
	Quote     textBlock `json:"quote"`
	Callout   textBlock `json:"callout"`
	Toggle    textBlock `json:"toggle"`
	Bulleted  textBlock `json:"bulleted_list_item"`
	Numbered  textBlock `json:"numbered_list_item"`
	ToDo      struct {
		RichText []richText `json:"rich_text"`
		Checked  bool       `json:"checked"`
	} `json:"to_do"`
	Code struct {
		RichText []richText `json:"rich_text"`
		Language string     `json:"language"`
	} `json:"code"`
}

type richText struct {
	PlainText   string `json:"plain_text"`
	Annotations struct {
		Code bool `json:"code"`
	} `json:"annotations"`
}
