# Locly film

The landing page plays a short narrated film, one per language:
`docs/assets/video/locly-en.mp4` and `docs/assets/video/locly-ar.mp4`
(with posters and caption tracks next to them). This folder is where they are made.

- `film/` is the film itself: one HTML composition (`index.html`, `film.css`,
  `film.js`) shared by both languages, and the two voiceovers in `film/voice/`.
- `export.mjs` renders it: every frame is drawn exactly at its time with
  `Film.seek(t)`, piped into ffmpeg, and laid over the voiceover plus a
  sound-effects track that is synthesized from the film's own cues
  (key clicks while text types, thuds on the big numbers, chimes).

```sh
go run ./tools/serve -dir . -port 8790 &
npm install --no-save playwright && npx playwright install chromium

node tools/motion/export.mjs --lang en --out docs/assets/video/locly-en.mp4 --poster docs/assets/video/locly-en.jpg
node tools/motion/export.mjs --lang ar --out docs/assets/video/locly-ar.mp4 --poster docs/assets/video/locly-ar.jpg
```

Options: `--size 1280x720` (any 16:9 size; the stage scales), `--fps 30`, `--crf 25`,
`--voice file.mp3`, `--music file` (mixed quietly under the voice), `--no-sfx`,
`--poster file.jpg --poster-at <seconds>`. Set `CHROMIUM_PATH` to use an existing
Chromium. Requires ffmpeg with libx264 and aac.

## Editing the timeline

Open `http://127.0.0.1:8790/tools/motion/film/?lang=ar` to preview (space plays,
arrow keys step, `?t=12.5` opens on a moment, `&audio=voice/ar.mp3` plays the voice along).

Timings are cues measured from the voiceovers (`CUES` in `film.js`), one set per
language, so the same scene lands on the same spoken word in English and Arabic.
Elements refer to cues by name: `data-v="p@ready+0.2/0.5"` animates `--p` from 0 to 1
starting 0.2 s after the word "ready" ("جاهز"), and `data-words="…"` gives each word of a
spoken line the time it is said. If a voiceover is re-recorded, re-measure its cues.

The film's fonts: Inter and IBM Plex Sans Arabic come from `docs/assets/fonts`;
JetBrains Mono (`film/fonts`, SIL Open Font License) is used only here.
