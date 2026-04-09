package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/rclod/notion-go"
)

const rateDelay = 350 * time.Millisecond

// Known Notion Project select options. Create fails if project not in this set.
var knownProjects = map[string]bool{
	"BA": true, "Xpand": true, "Infra": true, "Paynura": true,
}

// Notion status option IDs (stable across renames).
const (
	statusIDTodo       = "16e78b6b-43dd-42e6-a8a9-16c985b0513f"
	statusIDInProgress = "60a0c132-8e59-4ed2-aee4-3c4575c1c151"
	statusIDDone       = "81c9af53-e464-44af-8a32-da6ae5ccf3f7"
)

// statusOption maps a local status string to a Notion Option with ID set.
// Name is required by the API (the library serializes it without omitempty).
// We use a placeholder name; the ID takes precedence on the server side.
func statusOption(s string) notionapi.Option {
	switch s {
	case "In Progress":
		return notionapi.Option{ID: notionapi.PropertyID(statusIDInProgress)}
	case "Todo":
		return notionapi.Option{ID: notionapi.PropertyID(statusIDTodo)}
	case "Backlog":
		return notionapi.Option{ID: notionapi.PropertyID(statusIDTodo)}
	case "Done":
		return notionapi.Option{ID: notionapi.PropertyID(statusIDDone)}
	}
	return notionapi.Option{ID: notionapi.PropertyID(statusIDTodo)}
}

// NotionStatusID returns the Notion status option ID for a local status string.
func NotionStatusID(s string) string {
	return string(statusOption(s).ID)
}

// NotionClient wraps the Notion API client for Work Log operations.
type NotionClient struct {
	client     *notionapi.Client
	databaseID notionapi.DatabaseID
}

func NewNotionClient(apiKey, databaseID string) *NotionClient {
	return &NotionClient{
		client:     notionapi.NewClient(notionapi.Token(apiKey)),
		databaseID: notionapi.DatabaseID(databaseID),
	}
}

// Create makes a new page in the Work Log database. Returns the page ID.
func (c *NotionClient) Create(title, project, localStatus, effort, description string) (string, error) {
	if !knownProjects[project] {
		return "", fmt.Errorf("unknown Notion project %q (known: BA, Xpand, Infra, Paynura)", project)
	}

	now := notionapi.Date(time.Now())
	props := notionapi.Properties{
		"Task": notionapi.TitleProperty{
			Title: []notionapi.RichText{
				{Text: &notionapi.Text{Content: title}},
			},
		},
		"Project": notionapi.SelectProperty{
			Select: notionapi.Option{Name: project},
		},
		"Status": notionapi.StatusProperty{
			Status: statusOption(localStatus),
		},
		"Effort": notionapi.SelectProperty{
			Select: notionapi.Option{Name: effort},
		},
		"Date": notionapi.DateProperty{
			Date: &notionapi.DateObject{Start: &now},
		},
	}

	if description != "" {
		props["Description"] = notionapi.RichTextProperty{
			RichText: []notionapi.RichText{
				{Text: &notionapi.Text{Content: description}},
			},
		}
	}

	page, err := c.client.Page.Create(context.Background(), &notionapi.PageCreateRequest{
		Parent:     notionapi.Parent{DatabaseID: c.databaseID},
		Properties: props,
	})
	if err != nil {
		return "", fmt.Errorf("notion create: %w", err)
	}

	time.Sleep(rateDelay)
	return string(page.ID), nil
}

// Update patches properties on an existing page.
func (c *NotionClient) Update(pageID string, props notionapi.Properties) error {
	_, err := c.client.Page.Update(context.Background(), notionapi.PageID(pageID), &notionapi.PageUpdateRequest{
		Properties: props,
	})
	if err != nil {
		return fmt.Errorf("notion update: %w", err)
	}
	time.Sleep(rateDelay)
	return nil
}

// Archive soft-deletes a page by setting archived=true.
func (c *NotionClient) Archive(pageID string) error {
	_, err := c.client.Page.Update(context.Background(), notionapi.PageID(pageID), &notionapi.PageUpdateRequest{
		Archived: true,
	})
	if err != nil {
		return fmt.Errorf("notion archive: %w", err)
	}
	time.Sleep(rateDelay)
	return nil
}
