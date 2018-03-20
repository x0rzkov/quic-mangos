package quic

import (
	"crypto/tls"

	"github.com/SentimensRG/ctx"
	"github.com/go-mangos/mangos"
	quic "github.com/lucas-clemente/quic-go"
	"github.com/pkg/errors"
)

type dialMux struct {
	mux  dialMuxer
	sess *refcntSession
	sock mangos.Socket
}

func newDialMux(sock mangos.Socket, m dialMuxer) *dialMux {
	return &dialMux{sock: sock, mux: m}
}

func (dm *dialMux) LoadSession(n netlocator, tc *tls.Config, qc *quic.Config) error {
	dm.mux.Lock()
	defer dm.mux.Unlock()

	var ok bool
	if dm.sess, ok = dm.mux.GetSession(n); !ok {

		// We don't have a session for this [ ??? ] yet, so create it
		qs, err := quic.DialAddr(n.Netloc(), tc, qc)
		if err != nil {
			return err
		}

		// Init refcnt to track the Session's usage and clean up when we're done
		dm.sess = newRefCntSession(qs, dm.mux)
		dm.mux.AddSession(qs.RemoteAddr(), dm.sess) // don't add until it's incremented
	}

	dm.sess.Incr()
	return nil
}

func (dm dialMux) Dial(path string) (s quic.Stream, err error) {

	if s, err = dm.sess.OpenStreamSync(); err != nil {
		err = errors.Wrap(err, "open stream")
	} else {
		// There's no Close method for mangos.PipeDialer, so we need to decr
		// the ref counter when the stream closes.
		ctx.Defer(s.Context(), func() { _ = dm.sess.DecrAndClose() })
	}

	return
}

type dialer struct {
	netloc
	*dialMux
	opt  *options
	sock mangos.Socket
}

func (d dialer) Dial() (mangos.Pipe, error) {
	tc, qc := getQUICCfg(d.opt)

	if err := d.LoadSession(d.netloc, tc, qc); err != nil {
		return nil, errors.Wrap(err, "dial quic")
	}

	stream, err := d.dialMux.Dial(d.Path)
	if err != nil {
		return nil, errors.Wrap(err, "dial path")
	}

	return dialPipe(asPath(d.Path), stream, d.sock)
}

func (d dialer) GetOption(name string) (interface{}, error) { return d.opt.get(name) }
func (d dialer) SetOption(name string, v interface{}) error { return d.opt.set(name, v) }
