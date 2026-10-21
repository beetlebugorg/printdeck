package system

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Init is the first program at boot (init= on the kernel command line). The root file system is
// read-only. Init mounts the data partition, puts writable layers from it over /etc, /var, /home
// and /root, starts the start screen, and then starts systemd. It does not return.
func Init() {
	os.Setenv("PATH", "/usr/bin")
	syscall.Mount("proc", "/proc", "proc", 0, "")
	syscall.Mount("sysfs", "/sys", "sysfs", 0, "")
	syscall.Mount("devtmpfs", "/dev", "devtmpfs", 0, "")
	say("starting")

	data, err := Partition("printdeck-data")
	if err != nil {
		rescue("No printdeck-data partition")
	}
	if err := growData(data); err != nil {
		rescue(err.Error())
	}
	// e2fsck -p repairs what it can. An exit code of 4 or more is an error that it cannot repair.
	if err := exec.Command("e2fsck", "-p", "/dev/"+data).Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() >= 4 {
			rescue("The data partition has errors that e2fsck cannot repair")
		}
	}
	if err := syscall.Mount("/dev/"+data, "/data", "ext4", syscall.MS_NOATIME, ""); err != nil {
		rescue("Cannot mount the data partition")
	}

	if err := MergeAccounts("/data/etc", "/etc"); err != nil {
		say("merging the accounts: " + err.Error())
	}
	for _, d := range []string{"etc", "var", "home", "root"} {
		os.MkdirAll("/data/"+d, 0o755)
		os.MkdirAll("/data/.work/"+d, 0o755)
		opts := fmt.Sprintf("lowerdir=/%s,upperdir=/data/%s,workdir=/data/.work/%s", d, d, d)
		if err := syscall.Mount("overlay", "/"+d, "overlay", 0, opts); err != nil {
			rescue("Cannot put the writable layer over /" + d)
		}
	}

	// With "quiet" on the kernel command line, show the start screen until the deck starts. A
	// finger on the screen at boot shows the boot messages instead.
	if cmdline, _ := os.ReadFile(procCmdline); hasWord(string(cmdline), "quiet") {
		startSplash()
	}

	err = syscall.Exec("/usr/lib/systemd/systemd", []string{"/usr/lib/systemd/systemd"}, os.Environ())
	rescue("Cannot start systemd: " + err.Error())
}

// say writes a message to the kernel log, so it is in the journal but not on the start screen.
func say(msg string) {
	if f, err := os.OpenFile("/dev/kmsg", os.O_WRONLY, 0); err == nil {
		fmt.Fprintf(f, "printdeck-init: %s\n", msg)
		f.Close()
	}
}

// rescue reports a boot error on the console and on the config partition, where a computer can
// read it. After a tryboot into a new slot, it restarts, and the firmware starts the old slot. Else
// the debug image starts a shell. The release image has no shell: it waits, because the first
// program must not end.
func rescue(msg string) {
	say(msg)
	if con, err := os.OpenFile("/dev/console", os.O_WRONLY, 0); err == nil {
		fmt.Fprintf(con, "printdeck-init: %s\n", msg)
		con.Close()
	}
	if umount, err := MountConfig(); err == nil {
		log := "printdeck-init: " + msg + "\n"
		if out, err := exec.Command("dmesg").Output(); err == nil {
			log += string(out)
		} else if b, err := readKmsg(); err == nil {
			log += b
		}
		os.WriteFile(filepath.Join(configDir, "printdeck-boot.log"), []byte(log), 0o644)
		umount()
	}
	if Tryboot() {
		say("restarting into the old slot in 10 seconds")
		time.Sleep(10 * time.Second)
		syscall.Sync()
		syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
	}
	if _, err := os.Stat("/bin/sh"); err == nil {
		con, _ := os.OpenFile("/dev/console", os.O_RDWR, 0)
		if con != nil {
			fd := int(con.Fd())
			syscall.Dup3(fd, 0, 0)
			syscall.Dup3(fd, 1, 0)
			syscall.Dup3(fd, 2, 0)
		}
		syscall.Exec("/bin/sh", []string{"/bin/sh"}, os.Environ())
	}
	for {
		time.Sleep(time.Hour)
	}
}

// readKmsg reads the kernel log from /dev/kmsg, for an image with no dmesg.
func readKmsg() (string, error) {
	f, err := os.OpenFile("/dev/kmsg", os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var b strings.Builder
	buf := make([]byte, 8192)
	for {
		n, err := f.Read(buf)
		if err != nil {
			break
		}
		// "priority,sequence,time,flags;message"
		if _, msg, ok := strings.Cut(string(buf[:n]), ";"); ok {
			b.WriteString(msg)
		}
	}
	return b.String(), nil
}

// growData makes the data partition fill the disk at the first boot. The sizes are in 512-byte
// sectors. It grows only when more than 64 MiB is free after the partition. The partition is empty
// at the first boot, so it gets a new file system: resize2fs writes all the inode tables of the
// large disk at once, which takes minutes over USB. mke2fs with lazy inode tables takes seconds,
// and the kernel fills them in later.
func growData(data string) error {
	part := filepath.Join(sysBlock, data)
	target, err := filepath.EvalSymlinks(part)
	if err != nil {
		return err
	}
	disk := filepath.Base(filepath.Dir(target))
	num := readSysInt(filepath.Join(part, "partition"))
	start := readSysInt(filepath.Join(part, "start"))
	size := readSysInt(filepath.Join(part, "size"))
	end := readSysInt(filepath.Join(sysBlock, disk, "size"))
	if end-start-size <= 131072 {
		return nil
	}
	say("making the data partition fill the disk")
	if err := run("sfdisk", "--quiet", "--relocate", "gpt-bak-std", "/dev/"+disk); err != nil {
		return err
	}
	cmd := exec.Command("sfdisk", "--quiet", "--no-reread", "--no-tell-kernel", "-N", strconv.Itoa(num), "/dev/"+disk)
	cmd.Stdin = strings.NewReader(", +\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sfdisk: %s", strings.TrimSpace(string(out)))
	}
	if err := run("partx", "-u", "-n", strconv.Itoa(num), "/dev/"+disk); err != nil {
		return err
	}
	return run("mke2fs", "-q", "-F", "-t", "ext4", "-L", "data", "-E", "lazy_itable_init=1,lazy_journal_init=1", "/dev/"+data)
}

func readSysInt(path string) int {
	b, _ := os.ReadFile(path)
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func hasWord(s, w string) bool {
	for _, f := range strings.Fields(s) {
		if f == w {
			return true
		}
	}
	return false
}

// startSplash starts "printdeck splash" with the settings of the deck (deck.env), in the
// background. It keeps running under systemd until the deck starts.
func startSplash() {
	self, err := os.Executable()
	if err != nil {
		self = "/usr/bin/printdeck"
	}
	cmd := exec.Command(self, "splash")
	cmd.Env = append(os.Environ(), readEnvFile("/etc/printdeck/deck.env")...)
	cmd.Env = append(cmd.Env, readEnvFile(filepath.Join(stateDir, "settings.env"))...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		say("start screen: " + err.Error())
	}
}

// readEnvFile reads the KEY=VALUE lines of an environment file, with no comments and with no
// quotes around a value.
func readEnvFile(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var env []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		l := strings.TrimSpace(s.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok || k == "" {
			continue
		}
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		env = append(env, k+"="+v)
	}
	return env
}
