# Pull request

## What
<!-- One or two sentences describing the change. -->

## Why
<!-- The motivation: what does this PR teach or solve? -->

## What I learned
<!-- Specific to this project — write the lesson, not just the work. -->
<!-- Examples: -->
<!-- - "Discovered that gorilla/websocket buffers writes, which means a slow client can block the Hub. Fixed by giving each client a bounded outbound channel and dropping the client if it backs up." -->
<!-- - "Spent two hours debugging a context cancellation that wasn't propagating — turns out I was creating a new context with `context.Background()` instead of deriving from the request context." -->

## Testing
<!-- How did you verify this works? -->
- [ ] Manually tested locally
- [ ] Added/updated tests
- [ ] CI is green

## Checklist
- [ ] Comments explain *why*, not *what*
- [ ] All goroutines have a known stop condition
- [ ] Errors are wrapped with context
- [ ] No `fmt.Println` (use `slog`)
- [ ] No global state introduced
