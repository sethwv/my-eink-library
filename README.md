![my-sideload-library](docs/assets/brand/lockups/horizontal-on-forest.png)

A self-hosted EPUB library for e-readers. Browse a read-only library, download original EPUBs or Kobo-optimized KEPUBs, and manage access through a small authenticated web UI built for older e-reader browsers.
[Refer to the docs](https://sethwv.github.io/my-sideload-library/) for complete setup and deployment guides.

## Support

If this project has been useful, tips are appreciated.

[![Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/sethwv)


## Highlights

- Complatible with web-browser equipped Kobo devices
- Reads book metadata and covers directly from EPUB files
- Watches one or more library directories as read-only
- Supports multiple users, admin controls, favourites
- Converts EPUBs to KEPUB on demand
- Supports Hardcover & Chaptarr Integrations
  
| <img width="889" height="500" alt="Library grid in dark mode on a desktop browser" src="https://sideload.swvn.io/assets/images/generated/library-dark-desktop.png" /> | <img width="375" height="500" alt="Library grid in dark mode on a portrait e-reader browser" src="https://sideload.swvn.io/assets/images/generated/library-dark-ereader.png" /> |
| -- | -- |
| <img width="889" height="500" alt="Book details in light mode on a desktop browser" src="https://sideload.swvn.io/assets/images/generated/book-modal-desktop.png" /> | <img width="375" height="500" alt="Book details in light mode on a portrait e-reader browser" src="https://sideload.swvn.io/assets/images/generated/book-modal-ereader.png" /> |

## Run It

Create an ignored `docker-compose.override.yml` with your library mount:

```yaml
services:
  sideload-library:
    volumes:
      - /path/to/epubs:/library:ro
```

Then start the published image:

```bash
docker compose up -d
```

Put the service behind an HTTPS reverse proxy before signing in. On first startup, open the service to create the first administrator account in the one-time setup screen. Session cookies require HTTPS, while the supplied Compose file exposes the internal HTTP port for that proxy. The supplied Compose file persists application data in a named volume.

For local development, configuration details, and validation commands, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

This project is licensed under [AGPL-3.0-only](LICENSE).

## AI Disclosure

This project was developed with assistance from AI tools. However all changes are human reviewed, validated, and checked before merge/commit.
