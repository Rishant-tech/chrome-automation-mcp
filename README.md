# chrome-automation-mcp

A small MCP server for driving Google Chrome from Codex or any MCP client.

## Features

- Start Chrome in debug mode
- Attach to an existing Chrome debug session
- Open URLs and return a page snapshot
- Search the web
- Click, type, wait, evaluate JavaScript, and capture screenshots

## Requirements

- macOS
- Google Chrome installed
- Go 1.24+

## Run locally

From the repository root:

```bash
go run .
```

Or build a binary:

```bash
go build -o chrome-automation-mcp .
./chrome-automation-mcp
```

## Add to Codex CLI

If you are running the server locally, add it as a stdio MCP server:

```bash
codex mcp add chrome-automation -- /absolute/path/to/chrome-automation-mcp/chrome-automation-mcp
```

If you prefer to run from source during development:

```bash
codex mcp add chrome-automation -- bash -lc 'cd /absolute/path/to/chrome-automation-mcp && go run .'
```

## Notes

- `browser_open_url` is the most reliable tool when you want Chrome to open a page and return the visible content to the client.
- `browser_search` opens Google search results in the browser.
- `browser_start` can attach to an existing debug session using `attach_url`.
- If you want a truly shareable one-command setup for all users, you will need to host the server as a streamable HTTP MCP endpoint or publish a binary release.
