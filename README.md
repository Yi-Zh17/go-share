# go-share

A lightweight single-binary netdisk initially built for Raspberry Pi 4B. Browse, upload, download, and delete files over LAN from any browser.

## Build

```bash
go build -o go-share .
```

Requires Go 1.24+. The only Go dependency is `github.com/disintegration/imaging` for image thumbnails.

For **video thumbnails**, `ffmpeg` must be installed on the system. Image thumbnails work without it.

## Run

```bash
./go-share
```

The server listens on `:8080`. Open `http://<hostname>:8080` in a browser.

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

This is intended for a trusted LAN. There is no authentication, no HTTPS, and no user isolation. Anyone on the network can browse, upload, and delete files.
