package utils

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
)

func RetryTo(f func() error) error {
	timeout := time.After(time.Second * 60)
	step := time.Tick(time.Second * 2)
	var err error
	for {
		select {
		case <-step:
			if err = f(); err != nil {
				if errors.IsConflict(err) {
					continue
				}
				return err
			}
			return nil
		case <-timeout:
			return fmt.Errorf("Timout error, please try again:%v", err)
		}
	}
}

func RetryWithCount(f func() error, c int) error {
	var err error
	for i := 0; i < c; i++ {
		if err = f(); err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		return nil
	}
	return err
}
