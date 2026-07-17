package agent

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// streamClient carries long-lived log streams (a build's output, or a
// `docker logs -f` tail that stays open for minutes). It has no request
// timeout — the report client's 30s deadline would cut streams short. A
// per-stream context bounds each one instead.
var streamClient = &http.Client{}

// logSink is an io.Writer that does two things with every write: accumulate it
// (so the full text is available for the deploy record / TaskResult) and, if a
// stream is attached, forward it to the control plane in real time. Writes
// never block the producing command and never fail — if the live stream stalls
// or the server closes it, chunks are dropped from the wire but the buffer
// still holds everything.
type logSink struct {
	mu     sync.Mutex
	buf    strings.Builder
	ch     chan []byte // nil when no live stream is attached
	closed bool
	done   chan struct{}
	cancel context.CancelFunc // aborts the in-flight POST (unblocks a stalled pump)
}

// Write records p and best-effort forwards it to the live stream. The channel
// send is non-blocking under the same lock Close uses, so a full buffer drops
// the chunk from the wire (never from buf) and a concurrent Close can't race a
// send onto a closed channel.
func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.buf.Write(p)
	if !s.closed && s.ch != nil {
		b := append([]byte(nil), p...)
		select {
		case s.ch <- b:
		default: // live stream backpressured — buf still has it
		}
	}
	s.mu.Unlock()
	return len(p), nil
}

// WriteString lets callers append their own annotations (command echoes, state
// lines) through the same accumulate+stream path.
func (s *logSink) WriteString(str string) (int, error) { return s.Write([]byte(str)) }

// String returns everything accumulated so far.
func (s *logSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// Close ends the live stream (flushing what's queued) and returns the full
// accumulated text. Safe to call once; further writes are recorded but not
// streamed. It waits briefly for the pump to flush the tail, then force-aborts
// the request so a stalled server can never hang the caller (the agent's report
// loop calls this synchronously on the deploy path).
func (s *logSink) Close() string {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		if s.ch != nil {
			close(s.ch)
		}
	}
	s.mu.Unlock()
	if s.done != nil {
		select {
		case <-s.done: // pump flushed and closed the body cleanly
		case <-time.After(3 * time.Second):
			if s.cancel != nil {
				s.cancel() // stalled server: abort the POST and move on
			}
			<-s.done
		}
	}
	return s.String()
}

// newLogSink creates a sink. When streamID is empty the sink only accumulates.
// Otherwise it opens a chunked POST /api/agent/logstream whose body is fed live
// as output is written; kind is "deploy" or "runtime". The stream ends when the
// sink is closed or ctx is cancelled.
func (a *Agent) newLogSink(ctx context.Context, streamID, serviceID, kind string) *logSink {
	s := &logSink{}
	if streamID == "" {
		return s
	}
	s.ch = make(chan []byte, 512)
	s.done = make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	pr, pw := io.Pipe()

	// Pump: buffer -> pipe. Closing s.ch (via Close) ends it with EOF so the
	// POST completes cleanly; ctx cancellation aborts the request instead.
	go func() {
		defer close(s.done)
		for {
			select {
			case <-ctx.Done():
				_ = pw.CloseWithError(ctx.Err())
				return
			case b, ok := <-s.ch:
				if !ok {
					_ = pw.Close()
					return
				}
				if _, err := pw.Write(b); err != nil {
					// Server closed the stream; keep draining so Write's
					// non-blocking sends never wedge, but stop writing.
					_ = pr.CloseWithError(err)
					return
				}
			}
		}
	}()

	// The request itself. Its body is the pipe; it stays open until the pump
	// closes pw. Failure here is non-fatal — accumulation is unaffected. On any
	// exit it closes pr so a pump blocked in pw.Write always unblocks.
	go func() {
		defer pr.Close()
		url := strings.TrimSuffix(a.cfg.ServerURL, "/") + "/api/agent/logstream"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
		if err != nil {
			_ = pr.CloseWithError(err)
			return
		}
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("X-Rackops-Token", a.cfg.Token)
		req.Header.Set("X-Rackops-Stream", streamID)
		req.Header.Set("X-Rackops-Service", serviceID)
		req.Header.Set("X-Rackops-Kind", kind)
		req.Header.Set("X-Rackops-Node", a.nodeID)
		resp, err := streamClient.Do(req)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	return s
}

// maxFollowDuration caps a single `logs -f` tail so a browser tab that closes
// without a stop task (crash, network drop) can't leak a follower forever.
const maxFollowDuration = 15 * time.Minute

// startFollow begins streaming a service's runtime logs (docker/compose
// logs -f) to the control plane, keyed by p.StreamID. Re-following the same
// stream ID replaces the prior follower. Non-blocking: the tail runs in a
// goroutine so the agent's report loop is never held up.
func (a *Agent) startFollow(parent context.Context, p DeployPayload) {
	if a.runtime == nil || p.StreamID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(parent, maxFollowDuration)
	f := &follower{cancel: cancel}

	a.followMu.Lock()
	if old, ok := a.follows[p.StreamID]; ok {
		old.cancel() // replace an existing follower for this stream
	}
	a.follows[p.StreamID] = f
	a.followMu.Unlock()

	go func() {
		sink := a.newLogSink(ctx, p.StreamID, p.ServiceID, "runtime")
		var err error
		if p.ComposeYML != "" {
			err = a.runtime.FollowComposeLogs(ctx, p, 200, p.Since, sink)
		} else {
			err = a.runtime.FollowLogs(ctx, targetContainer(p), 200, p.Since, sink)
		}
		// ctx cancellation (stop task / timeout) is the normal end; only note
		// unexpected failures (e.g. container absent) into the stream.
		if err != nil && ctx.Err() == nil {
			sink.WriteString("\n(log stream ended: " + err.Error() + ")\n")
			log.Printf("agent: follow %s ended: %v", p.StreamID, err)
		}
		sink.Close()
		a.followMu.Lock()
		// Only remove our own registration; a newer follower for this stream
		// may have replaced us (map holds a different *follower then).
		if a.follows[p.StreamID] == f {
			delete(a.follows, p.StreamID)
		}
		a.followMu.Unlock()
		cancel()
	}()
}

// stopFollow cancels a live log follower by stream ID (no-op if unknown).
func (a *Agent) stopFollow(streamID string) {
	a.followMu.Lock()
	f := a.follows[streamID]
	delete(a.follows, streamID)
	a.followMu.Unlock()
	if f != nil {
		f.cancel()
	}
}

// closeFollows cancels every live follower (called on agent shutdown).
func (a *Agent) closeFollows() {
	a.followMu.Lock()
	for id, f := range a.follows {
		f.cancel()
		delete(a.follows, id)
	}
	a.followMu.Unlock()
}
