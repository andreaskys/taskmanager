# kbn

Terminal kanban in Go to organize day-to-day work. Single binary, data in local SQLite (`~/.local/share/kbn/kbn.db`), no server, no CGO.

    make install                 # installs to ~/.local/bin/kbn
    kbn                          # run
    kbn -theme nord              # default, light, monokai, gruvbox, nord, tokyonight, catppuccin, contrast, dracula (also $KBN_THEME; `space y` cycles and remembers)
    kbn -db <file>               # use another database
    kbn export [-format md] [-o file]   # every board as JSON (default) or Markdown
    kbn import file.json         # adds the boards of an export as NEW boards (never overwrites)
    kbn backup                   # copy the database now

## Keys (nvim-style: `space` is the leader)
Press `space` and a menu of the next keys appears at the bottom. `space h` opens the full cheat sheet (always up to date).

| Leader | Action | | Direct | Action |
|---|---|---|---|---|
| `space space` | search window (all boards, preview) | | `h/j/k/l` | move selection |
| `space n` | new card window (Project, Description, Notes) | | `H/L` | move card between columns |
| `space enter` | meeting notes (scratchpad) | | `J/K` | reorder (manual sort) |
| `space x` | delete card | | `dd` | delete card |
| `space e` | open details | | `enter` | details |
| `space t` | today view | | `p` | cycle priority |
| `space .` | pin as focus of the day | | `*` | same |
| `space f` / `space s` | priority filter / sort | | `u` / `ctrl+r` | undo / redo |
| `space u` / `space U` | undo / redo | | `T`, `f`, `s`, `n`, `/` | same as leader |
| `space r` | **sticky notes** (reminders) | | | |
| `space m` | manage columns | | `?` | footer key list |
| `space a` | archive the Done column | | `esc` | clear the priority filter |
| `space E` | export JSON + Markdown | | `q` | quit |
| `space c` | clear filters | | | |
| `space y` | cycle theme | | | |
| `space h` | help: every macro | | | |
| `space b` / `q` | boards / quit | | | |

## Cards
- **New card (`space n`):** floating window with a bordered box per field: **Project** (`tab` completes a project you already used), **Description** (the title) and **Notes · to-do**. `ctrl+t` fills the notes, tags and priority from a **template** (bug, feature, meeting, deploy, review; edit or add yours in `templates.json` next to the database). `ctrl+s` saves from any field.
- **Notes / details text:** free text where `[ ]` lines are checkboxes. `shift+enter` (or `alt+enter`) adds one, `alt+x` toggles the one under the cursor, `tab` / `shift+tab` indent / outdent for **nested sub-tasks**, `enter` keeps the indentation (on an empty `[ ]` it ends the list). In the card view `space` toggles a checkbox; toggling a parent toggles its sub-tasks.
- **Editing fields:** title, project, due date, tags (`tab` completes known tags) and column names use the same floating window.

## Boards and columns
- **Columns (`space m`):** create (inserted before the last one), rename, reorder (`J/K`), delete (cards move to the previous column). The **last column is Done**.
- **WIP limit (`w` in the columns window):** the header shows `count/limit` (yellow when full, red when over) and a full column refuses new cards and moves.
- **Archive:** Done cards older than 14 days are archived at startup (`$KBN_ARCHIVE_DAYS`, 0 = never); `space a` archives the whole Done column now. Archived cards leave the board but stay in the search window, and opening one restores it.
- **Focus of the day:** `*` pins a card (★). Pinned cards show in a bar under the header (all boards) and first in the today view.
- **Today (`T`):** focus, overdue, today and the next 7 days across all boards. `enter` jumps to the card, `x` marks it done.

## Search, filter, sort
**Search window (`space space`):** floating, like nvim's picker. Type to filter every card (archived included) by project, description, tags, notes and board (accent/case-insensitive, all words must match, `#tag` = tags only); the right pane previews it. `↑/↓`, `ctrl+n/p`, `pgup/pgdn` navigate, `enter` opens the card's details so you can edit it right away, `tab` only shows it on its board, `esc` closes.
`f` cycles the priority filter (off → high → medium+ → any); `s` cycles the sort (manual → due date → priority).

## Sticky notes
`space r` opens a window of colored sticky notes, kept outside the boards, to remember things. `n` new, `enter` edit (checkboxes and sub-tasks work as everywhere: `shift+enter`, `tab`, `alt+x`), `c` color, `p` pin (pinned first), `D` reminder date (the header shows 🔔 when one is due), `t` turns a note into a task (the note is removed), `dd` / `x` delete, `u` undo, `esc` closes. Notes are included in `kbn export` / `kbn import` and in backups.

## Undo and redo
`u` / `ctrl+r` (or `space u` / `space U`) undo and redo creates, deletes, moves, reorders, archives and edits. The last 200 steps are kept **in the database, so they survive closing the app**. Column changes and board deletion are not undoable (deleting asks first).

## Meeting notes
`space enter` opens a scratchpad (same list keys as above). `esc` closes it **keeping the draft** (saved in the database, flagged `📝 notes draft`); `ctrl+s` turns it into a task in the first column; `ctrl+x` twice discards it.

## Mouse
Click selects, double-click opens, wheel scrolls. **Drag a card** and a bar shows where it will land: drop it on any column at the exact position (also reorders inside a column). In card details, click a checkbox to toggle it.

## Backup, export, import
- **Automatic backup:** every start (at most one per 6 h) copies the database to `backups/` next to it, keeping the newest 14.
- **Export:** `space E` or `kbn export` writes JSON (lossless) and Markdown (readable, checkboxes become `- [ ]`).
- **Import:** `kbn import file.json` adds the boards as new boards (a name clash gets "(imported)").

## Themes
`default` (terminal colors), `light` (for light terminals), `monokai`, `gruvbox`, `nord`, `tokyonight`, `catppuccin`, `dracula` and `contrast` (the terminal's bright 16 colors with a solid selection bar, for accessibility). Add one in `internal/tui/theme.go`.

## Design notes
- **Footer = the one place for key hints.** It shows the hints of whatever window is open, on a single line; on a narrow terminal the least important ones are dropped and `…` is shown. Messages ("carried over 2 items") always appear above it.
- **Modal focus:** while a floating window is open, everything under it is dimmed; the footer stays sharp.
- **Responsive board:** each column needs at least 26 cells; on a narrower terminal fewer columns are shown, the window follows the selection and the header says `columns 1-3 of 4`.
- **Due dates read like people say them** (`2d late`, `today`, `tomorrow`, `Fri 16`, `16 Oct`) and the color carries the urgency (late red, today orange, soon cyan); a finished card is never shown as late.
- Empty columns say what to do (`n` adds a card); a brand-new board welcomes you.

## Layout
- `cmd/kbn` – entry point, CLI subcommands, startup chores
- `internal/store` – SQLite: boards, columns (WIP), cards, archive, undo log, backup, export/import
- `internal/tui` – Bubble Tea v2 UI (`model.go` keys, `view.go` rendering, `undo.go`, `text.go` checkbox parsing/editing, `today.go`, `columns_ui.go`, `newcard.go`, `search.go`, `scratch.go`, `mouse.go`, `theme.go`, `help.go`)

## Phone companion (`kbn serve`)

A small web page (installable PWA) and JSON API over the same database as the terminal app: today,
board, diary, notes, search and the weekly summary, from your phone. Changes made there show up in
the open terminal app within a couple of seconds.

```sh
kbn serve                       # listens on 127.0.0.1:8787
```

The first start creates the access token in `~/.config/kbn/token` (mode 0600) and prints it once.
Every API call needs `Authorization: Bearer <token>`; the page asks for it on the first visit and
keeps it in the browser. `kbn serve` refuses to listen on anything but loopback or a Tailscale
address (`100.64.0.0/10`), so it can never be exposed on the LAN by mistake.

### With Tailscale

1. Install Tailscale on the computer and on the phone, and log both into the same tailnet.
2. Keep `kbn serve` on loopback and let Tailscale publish it with HTTPS:
   `tailscale serve --bg 8787` (check with `tailscale serve status`).
3. On the phone open `https://<computer>.<tailnet>.ts.net`, paste the token, then "Add to Home Screen".
4. Run it as a service: see `deploy/kbn-serve.service` (create `~/.config/kbn` first, the sandbox needs it; then `systemctl --user enable --now kbn-serve`).
5. In the Tailscale admin ACLs allow only your phone to reach this machine's port 443.

Do not use `tailscale funnel`: that would put the board on the public internet.
The token can be rotated by deleting `~/.config/kbn/token` and restarting the service.
