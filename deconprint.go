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
	pipes  []io.Reader
}

func New(output io.Writer, lockoutDelay time.Duration) *Printer {
	return &Printer{output: output, delay: lockoutDelay}
}

func (p *Printer) Add(r io.Reader) {
	p.pipes = append(p.pipes, r)
}

func (p *Printer) Run() error {
	n := len(p.pipes)
	if n == 0 {
		return nil
	}
	ev := make(chan event, n*2)
	var wg sync.WaitGroup
	for i, r := range p.pipes {
		wg.Add(1)
		go func(pipeID int, rd io.Reader) {
			defer wg.Done()
			buf := make([]byte, 4096)
			for {
				n, err := rd.Read(buf)
				if n > 0 {
					cp := make([]byte, n)
					copy(cp, buf[:n])
					ev <- event{pipeID: pipeID, data: cp}
				}
				if err != nil {
					ev <- event{pipeID: pipeID, closed: true}
					return
				}
			}
		}(i, r)
	}
	go func() {
		wg.Wait()
		close(ev)
	}()

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

	drainQueue := func() {
		for _, e := range queue {
			if !e.closed && len(e.data) > 0 {
				p.output.Write(e.data)
			}
		}
	}

	for {
		select {
		case e, ok := <-ev:
			if !ok {
				if timer != nil {
					timer.Stop()
				}
				drainQueue()
				return nil
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
