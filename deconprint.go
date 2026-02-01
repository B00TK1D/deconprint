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
	output  io.Writer
	delay   time.Duration
	inbox   []event
	inboxMu sync.Mutex
	wake    chan struct{}
	nextID  int
	mu      sync.Mutex
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
	pending := make(map[int][]event)
	var waiting []int
	closedPipes := make(map[int]bool)
	var timer *time.Timer
	var timerCh <-chan time.Time

	inWaiting := func(pipeID int) bool {
		for _, w := range waiting {
			if w == pipeID {
				return true
			}
		}
		return false
	}

	flushPipe := func(pipeID int) {
		for _, e := range pending[pipeID] {
			p.output.Write(e.data)
		}
		delete(pending, pipeID)
	}

	handOff := func() {
		owner = -1
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		for len(waiting) > 0 {
			j := waiting[0]
			waiting = waiting[1:]
			flushPipe(j)
			if closedPipes[j] {
				continue
			}
			owner = j
			timer = time.NewTimer(p.delay)
			timerCh = timer.C
			return
		}
	}

	for {
		select {
		case <-p.wake:
			e, ok := p.dequeue()
			if !ok {
				continue
			}
			if e.closed {
				closedPipes[e.pipeID] = true
				if owner == e.pipeID {
					handOff()
				}
				continue
			}
			if owner == e.pipeID {
				p.output.Write(e.data)
				timer.Stop()
				timer.Reset(p.delay)
			} else {
				pending[e.pipeID] = append(pending[e.pipeID], e)
				if !inWaiting(e.pipeID) {
					waiting = append(waiting, e.pipeID)
				}
				if owner == -1 {
					handOff()
				}
			}
		case <-timerCh:
			timer, timerCh = nil, nil
			handOff()
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
