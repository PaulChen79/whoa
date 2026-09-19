# Assets

What the README expects to find here, and what is still missing.

| File | Status | Used by |
|---|---|---|
| `banner.svg` | ✅ present | the top of the README |
| `demo.gif` | ❌ **needed** | the hero block, links to `demo.mp4` |
| `demo.mp4` | ❌ **needed** | the full-quality recording |
| `social-preview.png` | ❌ **needed** | GitHub link previews (repo Settings, not the README) |

## `demo.gif` / `demo.mp4` — the hero recording

**The one thing worth showing:** an agent retrying a failing command, and whoa
cutting in. That is the whole product in about twelve seconds.

Record a real Claude Code session:

1. `whoa install && whoa doctor`
2. Break a test so it fails for a reason the agent will misdiagnose — a missing
   env var reads better than a syntax error, because retrying looks reasonable.
3. Ask the agent to fix it. Let it retry four times on its own.
4. Stop recording two seconds after whoa's line appears.

**Frame it tight.** Terminal only, no desktop, no browser, no tab bar. 1280×720
or wider, dark theme, a font large enough to read at half size on a phone. Do
not speed it up: the point is that whoa arrives *before* the fifth attempt, and
speeding it up hides the wait that makes it feel useful.

**Check before committing:** no absolute paths with your username, no API keys
in scrollback, no other repo names, no visible tabs. GIF under ~5 MB or GitHub
will be slow to load it; keep the MP4 as the quality version and link it.

## `social-preview.png` — 1280×640

Not referenced by the README. Upload it under **Settings → General → Social
preview**, or GitHub shows a generic card whenever the repo is linked.
`banner.svg` rasterised to 1280×640 works; keep the text in the middle 80%,
because the edges get cropped in some clients.

## Two more worth having, once there is something to show

- **A `whoa calibrate` screenshot**, for the "Is it any good?" section — but
  only once the corpus is non-zero. A screenshot reading `0 of 50` argues
  against the tool.
- **An architecture diagram**, if the ASCII one in the README stops being
  enough. It is currently doing the job, so this is not urgent.
