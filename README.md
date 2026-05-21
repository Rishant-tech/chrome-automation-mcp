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

Clone the repo and register it with Codex CLI:

```bash
git clone https://github.com/Rishant-tech/chrome-automation-mcp.git
cd chrome-automation-mcp
go build -o chrome-automation-mcp .
codex mcp add chrome-automation -- "$(pwd)/chrome-automation-mcp"
```

If you prefer to run from source during development:

```bash
codex mcp add chrome-automation -- bash -lc 'cd /absolute/path/to/chrome-automation-mcp && go run .'
```

## Sample Prompts

These are example natural-language prompts and the kind of short result you can return.

### Open LinkedIn in Chrome

- Prompt: `use chrome and open linkedin`
- Response: `LinkedIn opened in Chrome.`

### Open your LinkedIn profile and check content

- Prompt: `navigate to my profile and check content`
- Response: `Your profile is visible. The page shows your name, headline, location, current role, and open-to-work status.`

### Open the Jobs section

- Prompt: `navigate to jobs section`
- Response: `The Jobs page is open. The page shows the Jobs tab, job tracker, preferences, and suggested job listings.`

### Search the web

- Prompt: `search google.com`
- Response: `Google search results are open in Chrome.`

## Notes

- `browser_open_url` is the most reliable tool when you want Chrome to open a page and return the visible content to the client.
- `browser_search` opens Google search results in the browser.
- `browser_start` can attach to an existing debug session using `attach_url`.
- If you want a truly shareable one-command setup for all users, you will need to host the server as a streamable HTTP MCP endpoint or publish a binary release.
