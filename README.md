# my-eink-library

A self-hosted EPUB library for e-readers. Browse a read-only library, download original EPUBs or Kobo-optimized KEPUBs, and manage access through a small authenticated web UI built for older e-reader browsers.
[Refer to the docs](https://sethwv.github.io/my-eink-library/) for complete setup and deployment guides.

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
  
| <img width="889" height="500" alt="image" src="https://github.com/user-attachments/assets/bbbb81ff-5e40-4899-bf60-474c03f4a312" /> | <img width="375" height="500" alt="image" src="https://github.com/user-attachments/assets/d00115e0-a15c-41a5-9d3d-765a3df47942" /> |
| -- | -- |
| <img width="889" height="500" alt="image" src="https://github.com/user-attachments/assets/41a5248a-210b-4e16-8c3b-0582b488f5e6" /> | <img width="375" height="500" alt="image" src="https://github.com/user-attachments/assets/3f9f6986-847a-4972-95b2-d13f90cf0777" /> |

## Run It

Create an ignored `docker-compose.override.yml` with your credentials and library mount:

```yaml
services:
  eink-library:
    environment:
      LIBRARY_USER: reader
      LIBRARY_PASS: choose-a-password
      SESSION_SECRET: choose-a-long-random-secret
    volumes:
      - /path/to/epubs:/library:ro
```

Then start the published image:

```bash
docker compose up -d
```

Put the service behind an HTTPS reverse proxy before signing in. Session cookies require HTTPS, while the supplied Compose file exposes the internal HTTP port for that proxy. The supplied Compose file persists application data in a named volume and intentionally contains placeholder credentials.

For local development, configuration details, and validation commands, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

This project is licensed under [AGPL-3.0-only](LICENSE).

## AI Disclosure

This project was developed with assistance from AI tools. However all changes are human reviewed, validated, and checked before merge/commit.
