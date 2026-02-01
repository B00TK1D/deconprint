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
	output io.Writer
	delay  time.Duration
	ev     chan event
	nextID int
	mu     sync.Mutex
}

func New(output io.Writer, lockoutDelay time.Duration) *Printer {
	p := &Printer{
		output: output,
		delay:  lockoutDelay,
		ev:     make(chan event, 64),
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
			p.ev <- event{pipeID: pipeID, data: cp}
		}
		if err != nil {
			p.ev <- event{pipeID: pipeID, closed: true}
			return
		}
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
		case e := <-p.ev:
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
