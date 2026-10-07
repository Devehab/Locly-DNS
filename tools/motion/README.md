# Story animation export

`docs/` contains a ~35-second story animation ("Remember names, not numbers") that plays
on the landing page. This tool renders the same animation to MP4 for social media.

```sh
go run ./tools/serve -dir docs -port 8790 &
npm install --no-save playwright && npx playwright install chromium

node tools/motion/export.mjs --lang en --size 1920x1080 --out locly-en-16x9.mp4   # YouTube, LinkedIn, X
node tools/motion/export.mjs --lang ar --size 1920x1080 --out locly-ar-16x9.mp4
node tools/motion/export.mjs --lang en --size 1080x1080 --out locly-en-1x1.mp4    # Instagram, feeds
node tools/motion/export.mjs --lang ar --size 1080x1080 --out locly-ar-1x1.mp4
```

Options: `--lang en|ar`, `--size WxH`, `--fps 30`, `--url http://127.0.0.1:8790/`, `--out file.mp4`.
Set `CHROMIUM_PATH` to use an existing Chromium. Requires ffmpeg with libx264.

The timeline lives in `docs/index.html` (`data-v="name@start/duration"` attributes) and
`docs/assets/motion.js`; edit the copy or timings there and re-export.
