package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"github.com/nlopes/slack"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"
)

type Process struct {
	*os.Process
	Tty string
	Cwd string
}
type SlackMessage struct {
	user  string
	token string
}

var ErrInvalidNumber = fmt.Errorf("please enter a valid number")
var ErrProcNotRunning = fmt.Errorf("error: process is not running")

func FindByPid(pid int) (*Process, error) {
	proc := new(Process)

	var err error
	proc.Process, err = os.FindProcess(pid)
	if err != nil {
		return nil, err
	}

	pidStr := strconv.Itoa(proc.Pid)

	lsofOutput, err := exec.Command("lsof", "-p", pidStr).Output()
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(lsofOutput))
	for scanner.Scan() {
		words := strings.FieldsFunc(scanner.Text(), unicode.IsSpace)
		if words[3] == "cwd" {
			proc.Cwd = strings.TrimSpace(strings.Join(words[8:], " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return proc, nil
}

func FindByName(stdout io.Writer, stdin io.Reader, name string) (*Process, error) {
	psOutput, err := exec.Command("ps", "-e").Output()
	if err != nil {
		return nil, err
	}
	lowercaseOutput := bytes.ToLower(psOutput)

	var names []string
	scanner := bufio.NewScanner(bytes.NewReader(lowercaseOutput))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, name) {
			names = append(names, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	for i, name := range names {
		fmt.Printf("%d: %s\n", i, name)
	}

	procNumber := -1
	_, err = fmt.Fprintln(stdout, "\nThe correct process number:")
	checkErr(err)
	_, err = fmt.Fscanf(stdin, "%d", &procNumber)
	checkErr(err)

	if procNumber < 0 {
		return nil, ErrInvalidNumber
	}

	pid, err := strconv.Atoi(strings.TrimSpace(
		strings.FieldsFunc(names[procNumber], unicode.IsSpace)[0]),
	)
	if err != nil {
		return nil, err
	}

	return FindByPid(pid)
}

func (p *Process) HealthCheck() error {
	if err := p.Signal(syscall.Signal(0)); err != nil {
		return ErrProcNotRunning
	}
	return nil
}

func main() {

	pid := flag.Int("pid", -1, "the pid of the process to follow")
	slackToken := flag.String("token", "", "Slack bot token")
	slackUser := flag.String("user", "", "Slack ID")
	interval := flag.Int("interval", 100, "interval for health checking the process in milliseconds")
	procName := flag.String("name", "", "the name of the process to find a pid for")

	flag.Parse()

	if *pid == -1 && *procName == "" {
		log.Fatalf("pid or name flag not specified")
	}

	slackMessage := SlackMessage{user: *slackUser, token: *slackToken}
	var err error
	var proc *Process
	if *pid != -1 {

		proc, err = FindByPid(*pid)
		if err != nil {
			log.Fatalln(err)
		}
	} else {
		proc, err = FindByName(os.Stdout, os.Stdin, *procName)
		if err != nil {
			log.Fatalln(err)
		}
	}

	if err := proc.HealthCheck(); err != nil {
		log.Fatalln(err)
	}

	fmt.Print(proc)

	var running int64

	for {
		if atomic.LoadInt64(&running) == 0 {
			err = proc.HealthCheck()
			if err != nil {
				message := fmt.Sprintf("Process with pid %d finished", proc.Process.Pid)
				err := slackMessage.sendSlack(message)
				checkErr(err)
				fmt.Println("process finished")
				break
			}
		}
		
		time.Sleep(time.Millisecond * time.Duration(*interval))
	}
}

func (slackMessage *SlackMessage) sendSlack(msg string) error {
	api := slack.New(slackMessage.token)
	userID := slackMessage.user

	_, _, channelID, err := api.OpenIMChannel(userID)
	checkErr(err)
	_, _, err = api.PostMessage(channelID, msg, slack.PostMessageParameters{})
	if err != nil {
		return err
	}
	return nil

}

func checkErr(err error) {
	if err != nil {
		fmt.Printf("%s\n", err)
	}
}
