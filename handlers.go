// Package signals provides a registry for signal handler functions.
package signals // import "toolman.org/base/signals"

import (
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"toolman.org/base/log"
	"toolman.org/base/runtimeutil"
)

var (
	utx      sync.Mutex
	on       bool
	handlers map[os.Signal][]*handler
	nch      chan os.Signal
	done     chan struct{}
	down     chan struct{}
)

// Handler is a function for handling signals; it should be passed to either
// RegisterHandler or RegisterSoftHandler along with the list of signals it
// should handle. When called to handle a signal, the current signal being
// processed is provided as the sig argument. Handler should return true if
// further processing should occur for the current instance of this signal
// or false if further processing should be skipped.
type Handler func(sig os.Signal) bool

type handler struct {
	hdlr Handler
	soft bool
	info *runtimeutil.FunctionInfo
}

// Stop is called to stop all signal processing. When Stop returns, it is
// guaranteed that no further signal processing will occur.
func Stop() {
	if on {
		signal.Stop(nch)
		close(done)
		<-down
		on = false
	}
}

// RegisterHandler registers h as a hander for each signal s. When a signal is
// received, each of its registered handlers is called in the order in which
// they were registered -- until one of the handlers returns false or the list
// of handlers is exhausted.
func RegisterHandler(h Handler, s ...os.Signal) {
	register(&handler{h, false, runtimeutil.FuncID(h)}, s)
}

// RegisterSoftHandler registers h as a signal handler in the same manner as
// RegisterHandler however, if any subsequent handler is registered for any
// of the signals listed here, this handler will be replaced by the new
// handler instead of appending it to the list of registered handlers.
func RegisterSoftHandler(h Handler, s ...os.Signal) {
	register(&handler{h, true, runtimeutil.FuncID(h)}, s)
}

func register(h *handler, sl []os.Signal) {
	utx.Lock()
	defer utx.Unlock()

	log.V(2).Infof("registering handler: %v: signals: %v", h.info, sl)

	if !on {
		handlers = make(map[os.Signal][]*handler)
		done = make(chan struct{})
		down = make(chan struct{})
		nch = make(chan os.Signal)
	}

	for _, s := range sl {
		if _, ok := handlers[s]; !ok {
			signal.Notify(nch, s)
		}

		if lh := len(handlers[s]); lh > 0 && handlers[s][lh-1].soft {
			handlers[s][lh-1] = h
		} else {
			handlers[s] = append(handlers[s], h)
		}
	}

	if !on {
		go handleSignals()
		on = true
	}
}

func registerOne(f func(), s os.Signal) {
	RegisterHandler(func(os.Signal) bool { f(); return true }, s)
}

func OnSIGHUP(f func()) {
	registerOne(f, syscall.SIGHUP)
}

func OnSIGINT(f func()) {
	registerOne(f, syscall.SIGINT)
}

func OnSIGTERM(f func()) {
	registerOne(f, syscall.SIGTERM)
}

func OnSIGCHLD(f func()) {
	registerOne(f, syscall.SIGCHLD)
}

func OnSIGUSR1(f func()) {
	registerOne(f, syscall.SIGUSR1)
}

func OnSIGUSR2(f func()) {
	registerOne(f, syscall.SIGUSR2)
}

func OnSIGWINCH(f func()) {
	registerOne(f, syscall.SIGWINCH)
}

func handleSignals() {
	log.V(1).Info("Handling signals")
	for {
		select {
		case <-done:
			log.V(1).Info("No longer handling signals")
			close(down)
			return

		case s := <-nch:
			var id string
			if log.V(2) {
				id = strconv.FormatInt(time.Now().UnixNano(), 36)
				log.Infof("%s: handling signal %d", id, s)
			}

			func(s os.Signal) {
				log.V(2).Infof("%s: in handler subfunc", id)
				utx.Lock()
				defer utx.Unlock()
				log.V(2).Infof("%s: lock acquired", id)
				for _, h := range handlers[s] {
					log.V(2).Infof("%s: calling handler: %s", id, h.info.Name())
					if !h.hdlr(s) {
						log.V(2).Infof("%s: handler %s: returned false", id, h.info.Name())
						return
					}
					log.V(2).Infof("%s: handler %s: returned true", id, h.info.Name())
				}
			}(s)

			log.V(2).Infof("%s: done handling signal %d", id, s)
		}
	}
}
