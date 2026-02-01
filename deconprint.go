package deconprint

import (
	"io"
	"sync"
	"time"
)

type event struct {
	pipeID int
	data   []byte
	closed bool
}

type Printer struct {
	output   io.Writer
	delay    time.Duration
	inbox    []event
	inboxMu  sync.Mutex
	wake     chan struct{}
	nextID   int
	mu       sync.Mutex
}

func New(output io.Writer, lockoutDelay time.Duration) *Printer {
	p := &Printer{
		output: output,
		delay:  lockoutDelay,
		wake:   make(chan struct{}, 1),
	}
	go p.arbiter()
	return p
}

func (p *Printer) Add(r io.Reader) {
	p.mu.Lock()
	pipeID := p.nextID
	p.nextID++
	p.mu.Unlock()
	go p.readPipe(pipeID, r)
}

func (p *Printer) readPipe(pipeID int, r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			cp := make([]byte, n)
			copy(cp, buf[:n])
			p.enqueue(event{pipeID: pipeID, data: cp})
		}
		if err != nil {
			p.enqueue(event{pipeID: pipeID, closed: true})
			return
		}
	}
}

func (p *Printer) enqueue(e event) {
	p.inboxMu.Lock()
	p.inbox = append(p.inbox, e)
	p.inboxMu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Printer) arbiter() {
	var owner int = -1
	var queue []event
	var timer *time.Timer
	var timerCh <-chan time.Time

	flushNext := func() {
		if len(queue) == 0 {
			owner = -1
			return
		}
		e := queue[0]
		queue = queue[1:]
		owner = e.pipeID
		p.output.Write(e.data)
		timer = time.NewTimer(p.delay)
		timerCh = timer.C
	}

	for {
		select {
		case <-p.wake:
			e, ok := p.dequeue()
			if !ok {
				continue
			}
			if e.closed {
				if owner == e.pipeID {
					if timer != nil {
						timer.Stop()
						timer = nil
					}
					owner = -1
					flushNext()
				}
				continue
			}
			switch owner {
			case -1:
				owner = e.pipeID
				p.output.Write(e.data)
				timer = time.NewTimer(p.delay)
				timerCh = timer.C
			case e.pipeID:
				p.output.Write(e.data)
				timer.Stop()
				timer.Reset(p.delay)
			default:
				queue = append(queue, e)
			}
		case <-timerCh:
			timer, timerCh = nil, nil
			owner = -1
			flushNext()
		}
	}
}

func (p *Printer) dequeue() (event, bool) {
	p.inboxMu.Lock()
	defer p.inboxMu.Unlock()
	if len(p.inbox) == 0 {
		return event{}, false
	}
	e := p.inbox[0]
	p.inbox = p.inbox[1:]
	if len(p.inbox) > 0 {
		select {
		case p.wake <- struct{}{}:
		default:
		}
	}
	return e, true
}
