package main

import (
	"os/exec"
	"strings"
)


type AsyncExecData struct {
	output []byte
	error  error
}

func runthing(thing string, arg ...string) string {
	l.Infof("running thing: %s, %v", thing, arg)
	cmd := exec.Command(thing, arg...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		l.Error(err)
	}

	go func() {
		defer stdin.Close()
	}()

	out, err := cmd.CombinedOutput()
	if err != nil {
		l.Errorf("runthing: %s, %v: error:%s (%s)", thing, arg, err, strings.Trim(string(out), "\n "))
		return "error"
	}

	l.Infof("runthingoutput:\n%s\n", out)
	return string(out)
}

func runthing_async(ch chan<- AsyncExecData, thing string, arg ...string) string {
	l.Tracef("runthing_async: %s, %v", thing, arg)
	cmd := exec.Command(thing, arg...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		l.Error(err)
	}

	go func() {
		defer stdin.Close()
	}()

	data, err := cmd.CombinedOutput()
	if err != nil {
		l.Error(err)
	}

	l.Infof("runthing_async output:\n%s\n", data)
	ch <- AsyncExecData{
		error:  err,
		output: data,
	}
	return string(data)
}
