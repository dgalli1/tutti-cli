# tutti-cli

Unofficial command-line interface for [tutti.ch](https://www.tutti.ch) — Switzerland's largest classified ads platform.

> **Disclaimer:** This project is not affiliated with, endorsed by, or associated with tutti.ch or SMG Swiss Marketplace Group AG. Use responsibly and in accordance with tutti.ch's [Terms of Service](https://www.tutti.help/hc/de/articles/115003331547).

## Features

- **Full-text search** — same results as the website, fetched directly from the GraphQL API
- **Regexp filtering** — fetch a broad query, filter client-side with a regexp (`--regexp "m1|m2|air"`)
- **Price range filter** — `--min-price` / `--max-price` in CHF
- **Location filter** — canton or city substring match
- **Sorting** — `newest`, `oldest`, `price_asc`, `price_desc`
- **Multi-page fetch** — `--pages 3` fetches up to 90 results in one command
- **Price analysis** — shows median/quartiles across all tracked results and labels each listing (very cheap / below median / above median / expensive)
- **Local price database** — results are saved to `~/.tutti/db.sqlite` and tracked across searches; price changes are recorded
- **ASCII image previews** — `--with-previews` fetches listing thumbnails and renders them as ANSI true-color ASCII art in the terminal; in `--md` mode it embeds the image URL instead
- **Multiple output formats** — plain text (default), `--json`, `--md` (Markdown table)

## Installation

Requires Go 1.22+.

```bash
git clone https://github.com/gado-ships-it/tutti-cli.git
cd tutti-cli
go build -o /usr/local/bin/tutti .
```

Or with Homebrew (manual tap):

```bash
go install github.com/gado-ships-it/tutti-cli@latest
```

## Usage

```bash
# Basic search
tutti search "iphone 15"

# Regexp filter (client-side, case-insensitive)
tutti search "macbook" --regexp "pro|air"

# Price range
tutti search "macbook" --min-price 300 --max-price 1500

# Location filter
tutti search "velo" --location "zürich"

# Sort by price ascending
tutti search "fahrrad" --sort price_asc

# Fetch multiple pages (3 × 30 = up to 90 results)
tutti search "möbel" --pages 3

# Combine filters
tutti search "macbook" --regexp "m[123]" --min-price 400 --max-price 1200 --location "zürich" --sort price_asc --pages 2

# Markdown output (great for piping to a file or Obsidian)
tutti search "iphone 15 pro" --md

# ASCII image previews (ANSI true-color in terminal, image links in --md)
tutti search "macbook air" --with-previews --limit 5
tutti search "iphone 15 pro" --md --with-previews

# JSON output
tutti search "iphone 15" --json | jq '.listings[].formattedPrice'

# Price history from local DB
tutti history "iphone"
tutti history --stats "macbook"

# Debug: show the URL token for a query
tutti token "iphone 15 pro"
```

## Commands

| Command | Description |
|---------|-------------|
| `search <query>` | Search tutti.ch listings |
| `history [query]` | Browse locally tracked listings |
| `token <query>` | Print the encoded URL token (debug) |

### `search` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--regexp` | — | Client-side regexp filter on title + body |
| `--min-price` | 0 | Minimum price in CHF |
| `--max-price` | 0 | Maximum price in CHF |
| `--location` | — | Location substring filter (city or canton) |
| `--category` | — | Category ID filter |
| `--sort` | `newest` | `newest`, `oldest`, `price_asc`, `price_desc` |
| `--limit` | 30 | Results per page (max ~100) |
| `--pages` | 1 | Number of pages to fetch |
| `--no-save` | false | Skip saving results to local database |
| `--json` | false | Output raw JSON |
| `--md` | false | Output Markdown |
| `--with-previews` | false | Render thumbnails as ANSI ASCII art (terminal) or `![img](url)` (--md) |

## Example output

```
tutti.ch search: "macbook" (regexp filter: (?i)pro|air)
Found 1847 total listings, showing 5

Price analysis (12 listings in DB for this query):
  Min: CHF 349  |  Median: CHF 750  |  Max: CHF 1250
  Mean: CHF 709  |  25th%: CHF 429  |  75th%: CHF 769

────────────────────────────────────────────────────────────────────────────────
[1] MacBook Air 2020 M1
    Price: 429.-  [very cheap (bottom 25%, median CHF 750)]
    Location: Bazenheid (SG)  |  34m ago  |  Seller: D.I
    Im Dezember 2021 bei Mediamarkt gekauft. Kaum benutzt nur etwa 10 mal voll aufgeladen...
    https://www.tutti.ch/de/q/details/st-gallen/computer-zubehoer/computer/macbook-air-2020-m1
────────────────────────────────────────────────────────────────────────────────
```

## How it works

tutti.ch uses a GraphQL API (`/api/v10/graphql`) with React Query on the frontend. This CLI reverse-engineers the API including the custom URL search token encoding (a ROL2-based binary format with a length prefix) and the required request headers. Results are identical to what you see in the browser.

Price data is persisted locally in `~/.tutti/db.sqlite` (SQLite). Each time you run a search the database grows, making the price analysis more accurate over time.

## License

MIT — see [LICENSE](LICENSE).

## Contributing

Pull requests welcome. Issues welcome. This is an unofficial tool — API compatibility may break if tutti.ch changes their backend.
