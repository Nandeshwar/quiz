# quiz

USCIS civics quiz app built with Go and Echo.

## Local run

```bash
go run main.go
```

The app uses `PORT` when provided. Locally it defaults to `9999`.

## Render deployment

This repo includes [render.yaml](/Users/NANDESHWAR.SAH/src/quiz/render.yaml) for a Render web service.

Recommended settings:

- Runtime: `Go`
- Build Command: `go build -o quiz main.go`
- Start Command: `./quiz`
- Health Check Path: `/health`

Render will provide the `PORT` environment variable automatically.
