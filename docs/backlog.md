# Backlog

> Items observed during development that need to be evaluated for possible
> promotion to stories in future phases. These are not bugs or planned features — they are
> improvements, ideas, and adjustments that came up along the way.

---

## Performance

- **Video seek is slow:** navigating to a specific point on the video timeline has
  high latency. Investigate the cause (asset handler, encoding, buffering)
  and consider optimizations such as preloading segments, transcoding, or adjustments to the
  `<video>` element. **Update 28/08/2026:** video on Linux didn't even play until this
  date (see `docs/fase-1-mvp.md`) — the asset handler switched from an `application.Middleware`
  in Wails (`wails://` scheme) to a real `http.Server` on loopback
  (`services/video_server.go`). Reassess whether this item still applies with the
  new server before investigating further.

## UX — Transcript

- **Side-by-side columns for speech:** option to display tutor and student speech
  in two synchronized columns (tutor on the left, student on the right), keeping
  scrolling and the highlight of the current segment aligned between the columns. Alternative to
  the current linear view to make the conversation flow easier to read.

- **Transcript editing:** ability to correct errors in the transcript text
  directly in the UI (e.g., words in another language that the STT didn't recognize
  correctly, slang, truncated segments). Consider whether the correction updates only
  the displayed text or also the database entry.
