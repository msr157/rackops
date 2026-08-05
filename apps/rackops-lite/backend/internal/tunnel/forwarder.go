package tunnel

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"time"
)

// Forwarder runs the customer side of a live session: it accepts streams the
// POP opens and bridges each one to a local address.
//
// It is in this package rather than in the agent so that the two halves of
// the data path — the POP's dial and the customer's accept — sit next to
// each other. A change to the per-stream handshake that updates only one
// side is a class of bug that is very hard to see in review and very easy to
// see here.
type Forwarder struct {
	// Target resolves the local address to bridge a stream to, given the
	// public hostname the visitor asked for. Returning an error refuses the
	// stream with a message that reaches the visitor's error page.
	Target func(host string) (string, error)
	// DialTimeout bounds the connection to the local backend.
	DialTimeout time.Duration
	// OnBytes, if set, is called after each stream closes with the bytes
	// moved in each direction. Used to report transfer back to the console.
	OnBytes func(up, down int64)

	streams atomic.Int64
	bytesUp atomic.Int64
	bytesDn atomic.Int64
}

// Serve accepts streams until the session ends or ctx is cancelled. It
// returns the error that ended the session.
func (f *Forwarder) Serve(ctx context.Context, sess Session) error {
	if f.DialTimeout <= 0 {
		f.DialTimeout = 10 * time.Second
	}
	for {
		st, err := sess.Accept(ctx)
		if err != nil {
			// A closed session is the normal end of the loop, not a fault.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		go f.handle(ctx, st)
	}
}

// Stats reports live counters for the console.
func (f *Forwarder) Stats() (openStreams, up, down int64) {
	return f.streams.Load(), f.bytesUp.Load(), f.bytesDn.Load()
}

// handle bridges one stream to a local backend.
func (f *Forwarder) handle(ctx context.Context, st Stream) {
	f.streams.Add(1)
	defer f.streams.Add(-1)
	defer st.Close()

	// Bound the handshake so a POP that opens a stream and stalls cannot
	// pin a goroutine and a local socket indefinitely.
	_ = st.SetDeadline(time.Now().Add(15 * time.Second))
	hdr, err := ReadStreamHeader(st)
	if err != nil {
		return
	}

	target, err := f.resolve(hdr.Host)
	if err != nil {
		// Refuse politely: the POP turns this into a 502 with the reason,
		// instead of the visitor watching a connection die mid-response.
		_ = WriteStreamAccept(st, StreamAccept{Error: err.Error()})
		return
	}

	dialCtx, cancel := context.WithTimeout(ctx, f.DialTimeout)
	local, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", target)
	cancel()
	if err != nil {
		_ = WriteStreamAccept(st, StreamAccept{Error: "the app is not accepting connections"})
		return
	}
	defer local.Close()

	if err := WriteStreamAccept(st, StreamAccept{OK: true}); err != nil {
		return
	}
	// Clear the deadline: what follows may legitimately be a long download,
	// a websocket, or a server-sent event stream that stays open for hours.
	_ = st.SetDeadline(time.Time{})

	up, down := Splice(st, local)
	f.bytesUp.Add(up)
	f.bytesDn.Add(down)
	if f.OnBytes != nil {
		f.OnBytes(up, down)
	}
}

// resolve maps a public hostname to a local backend address.
func (f *Forwarder) resolve(host string) (string, error) {
	if f.Target == nil {
		return "", errors.New("this machine has no Front Door target configured")
	}
	target, err := f.Target(host)
	if err != nil {
		return "", err
	}
	if target == "" {
		return "", errors.New("no app is published at this address")
	}
	return target, nil
}
