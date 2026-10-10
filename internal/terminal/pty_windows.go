//go:build windows

package terminal

import (
	"os"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// conPty is a program in a pseudo-console of Windows, ConPTY: its output
// read from a pipe, its input written to another.
type conPty struct {
	hpc     windows.Handle
	in, out *os.File
	process windows.Handle
	once    sync.Once
}

// startPty runs a shell in a pseudo-console.
func startPty(shell, dir string, env []string, cols, rows int) (ptyProc, error) {
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, err
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		windows.CloseHandle(inR)
		windows.CloseHandle(inW)
		return nil, err
	}
	var hpc windows.Handle
	size := windows.Coord{X: int16(cols), Y: int16(rows)}
	if err := windows.CreatePseudoConsole(size, inR, outW, 0, &hpc); err != nil {
		for _, h := range []windows.Handle{inR, inW, outR, outW} {
			windows.CloseHandle(h)
		}
		return nil, err
	}
	// The console has its ends of the pipes.
	windows.CloseHandle(inR)
	windows.CloseHandle(outW)
	p := &conPty{hpc: hpc, in: os.NewFile(uintptr(inW), "conpty-in"), out: os.NewFile(uintptr(outR), "conpty-out")}

	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		p.Close()
		return nil, err
	}
	defer attrs.Delete()
	// The attribute's value is the console's handle itself.
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&hpc)), unsafe.Sizeof(hpc)); err != nil {
		p.Close()
		return nil, err
	}
	si := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(si))
	cmdline, err := syscall.UTF16PtrFromString(syscall.EscapeArg(shell))
	if err != nil {
		p.Close()
		return nil, err
	}
	var cwd *uint16
	if dir != "" {
		if cwd, err = syscall.UTF16PtrFromString(dir); err != nil {
			p.Close()
			return nil, err
		}
	}
	block := envBlock(env)
	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	if err := windows.CreateProcess(nil, cmdline, nil, nil, false, flags, &block[0], cwd, &si.StartupInfo, &pi); err != nil {
		p.Close()
		return nil, err
	}
	windows.CloseHandle(pi.Thread)
	p.process = pi.Process
	return p, nil
}

// envBlock is an environment as CreateProcess takes it: KEY=value
// strings, each ended with a NUL, and a NUL after them.
func envBlock(env []string) []uint16 {
	var out []uint16
	for _, kv := range env {
		if strings.IndexByte(kv, 0) >= 0 {
			continue
		}
		out = append(out, utf16.Encode([]rune(kv))...)
		out = append(out, 0)
	}
	return append(out, 0)
}

func (p *conPty) Read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *conPty) Write(b []byte) (int, error) { return p.in.Write(b) }

func (p *conPty) Resize(cols, rows int) error {
	return windows.ResizePseudoConsole(p.hpc, windows.Coord{X: int16(cols), Y: int16(rows)})
}

func (p *conPty) Wait() error {
	if p.process == 0 {
		return nil
	}
	_, err := windows.WaitForSingleObject(p.process, windows.INFINITE)
	return err
}

func (p *conPty) Kill() {
	if p.process != 0 {
		windows.TerminateProcess(p.process, 1)
	}
}

// Close closes the console, which ends the output's pipe, and the pipes.
func (p *conPty) Close() error {
	p.once.Do(func() {
		windows.ClosePseudoConsole(p.hpc)
		p.in.Close()
		p.out.Close()
	})
	return nil
}
