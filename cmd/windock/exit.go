package main

import (
	"fmt"
	"time"

	"github.com/BjarkeM/windock-go/internal/ipc"
)

const exitGrace = 10 * time.Second

func cmdExit() error {
	running, err := ipc.SignalExit()
	if err != nil {
		return err
	}
	if !running {
		fmt.Println("windock is not running")
		return nil
	}
	if err := ipc.WaitGone(exitGrace); err != nil {
		return err
	}
	fmt.Println("windock has stopped")
	return nil
}
