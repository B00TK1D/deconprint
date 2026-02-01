// Merges multiple command stdout/stderr streams to os.Stdout with
// lock-out so one stream's burst isn't interleaved with another.
package main

import (
	"github.com/B00TK1D/deconprint"
	"os"
	"os/exec"
	"time"
)

func main() {
	p := deconprint.New(os.Stdout, 30*time.Millisecond)

	cmd1 := exec.Command("sh", "-c", `echo "cmd1 stdout"; sleep 0.05; echo "cmd1 stdout 2"`)
	cmd2 := exec.Command("sh", "-c", `echo "cmd2 stderr" >&2; sleep 0.03; echo "cmd2 stderr 2" >&2`)

	stdout1, _ := cmd1.StdoutPipe()
	stderr2, _ := cmd2.StderrPipe()
	p.Add(stdout1)
	p.Add(stderr2)

	if err := cmd1.Start(); err != nil {
		os.Exit(1)
	}
	if err := cmd2.Start(); err != nil {
		os.Exit(1)
	}
	_ = p.Run()
	_ = cmd1.Wait()
	_ = cmd2.Wait()
}
