# go-share

A lightweight single-binary netdisk initially built for Raspberry Pi 4B. Browse, upload, download, and delete files over LAN from any browser.

## Build

```bash
go build -o go-share .
```

Requires Go 1.24+. Dependencies: `github.com/disintegration/imaging` for image thumbnails and `golang.org/x/term` for hidden password input.

For **video thumbnails**, `ffmpeg` must be installed on the system. Image thumbnails work without it.

## Run

```bash
./go-share
```

The server listens on `:8080`. Open `http://<hostname>:8080` in a browser. The browser will prompt for a password (HTTP Basic Auth).

On **first run** the server asks you in the terminal to set your own password (input hidden, typed twice for confirmation) and saves it to `auth.txt` next to the binary. Run it in a terminal that first time — if the server is started without one (e.g. as a service), create `auth.txt` manually with the password on a single line.

All shared files live in the `./folder` directory. Uploaded files land there. A `.cache` subdirectory is created automatically for thumbnails.

## Configuration

Edit the constants in `main.go`:

| Constant | Default | Purpose |
|---|---|---|
| `folder` | `"./folder"` | Shared directory on disk |
| `prefix` | `"/folder/"` | URL prefix for raw file serving |
| `port` | `":8080"` | Listen address |

## Usage

- **Browse** — click folders to navigate, click images/videos to open the lightroom
- **Upload** — click **Add** in the header, select files
- **Select mode** — click **Select** to toggle checkboxes, then **Download** or **Delete** in bulk
- **Lightroom** — click any image to open it full-screen:
  - **Scroll wheel** to zoom in/out (anchored at cursor)
  - **Click and drag** to pan when zoomed
  - **Double-click** or **0** key to reset zoom
  - **+/- buttons** or **+/- keys** to zoom by step
  - **Left/Right arrows** to navigate between images
  - **Delete button** to remove the current image
  - **Escape** or click backdrop to close

## Security

Every request (pages, APIs, and raw file downloads) is gated by a single password via HTTP Basic Auth. This is meant to keep random devices on the LAN out, not to be a hardened auth system.

- **Password storage** — `auth.txt` in the run directory (one line, plaintext). Keep it out of `./folder`; back it up if you like.
- **Change the password** — edit `auth.txt` and restart.
- **Reset (forgot password)** — delete `auth.txt` and restart; you will be prompted to set a new password.
- The username field in the browser prompt is ignored — enter anything, the password is all that matters.
- There is **no HTTPS** — the password and all files travel the LAN in plaintext (the password is base64-encoded in the `Authorization` header). Fine for a trusted home network; otherwise put the server behind a reverse proxy with TLS.
