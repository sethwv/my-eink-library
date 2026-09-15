# my-eink-library

A self-hosted EPUB library for e-readers. Browse a read-only library, download original EPUBs or Kobo-optimized KEPUBs, and manage access through a small authenticated web UI built for older e-reader browsers.

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

## Documentation

[See the docs site](https://sethwv.github.io/my-eink-library/) for setup and deployment guidance.

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

Visit `http://localhost:8080`. The supplied Compose file persists application data in a named volume and intentionally contains placeholder credentials.

For local development, configuration details, and validation commands, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

This project is licensed under [AGPL-3.0-only](LICENSE).

## AI Disclosure

This project was developed with assistance from AI tools. However all changes are human reviewed, validated, and checked before merge/commit.
