package um

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"
)

type platter uint32

type UniversalMachine struct {
	Gpr          [8]platter
	Heap         map[platter][]platter
	NextHeap     platter
	ExFinger     int
	done         chan error
	consoleIn    io.Reader
	consoleOut   io.Writer
	logger       *log.Logger
	backupFolder string
	lastBackup   time.Time
}

func New(input io.Reader, output io.Writer, errorOutput io.Writer, backupFolder string) *UniversalMachine {
	um := UniversalMachine{
		Gpr:          [8]platter{},
		Heap:         map[platter][]platter{},
		done:         make(chan error),
		consoleIn:    input,
		consoleOut:   output,
		logger:       log.New(errorOutput, "", log.LstdFlags),
		backupFolder: backupFolder,
	}
	return &um
}

func (um *UniversalMachine) backup() {
	if um.backupFolder != "" {
		if time.Since(um.lastBackup) > time.Minute {
			um.doBackup(time.Since(um.lastBackup) > 15*time.Minute)
			um.lastBackup = time.Now()
		}
	}
}
func (um *UniversalMachine) doBackup(withTimestampFile bool) {
	var err error
	var tmpFile *os.File

	tmpFile, err = ioutil.TempFile(um.backupFolder, "backup")
	if err != nil {
		log.Print("error creating backup file:", err)
		return
	}

	enc := gob.NewEncoder(tmpFile)
	err = enc.Encode(um)
	if err != nil {
		log.Printf("error encoding backup, (data left in %s): %s", tmpFile.Name(), err.Error())
		tmpFile.Close()
		return
	}

	if withTimestampFile {
		// also copy backup data to timestamped file, ignore any errors
		var tsFile *os.File
		tsFile, _ = os.Create(filepath.Join(um.backupFolder, time.Now().Format("backup.2006-01-02T15:04:05.dat")))
		tmpFile.Seek(0, io.SeekStart)
		io.Copy(tsFile, tmpFile)
		tsFile.Close()
	}

	tmpFile.Close()

	backupFile := filepath.Join(um.backupFolder, "backup.dat")
	err = os.Rename(tmpFile.Name(), backupFile)
	if err != nil {
		log.Printf("error renaming backup file, (data left in %s): %s", tmpFile.Name(), err.Error())
	}
}

func (um *UniversalMachine) LoadFromBackup(b io.Reader) error {
	dec := gob.NewDecoder(b)
	err := dec.Decode(um)
	if err != nil {
		return err
	}
	um.ExFinger--
	return nil
}
func (um *UniversalMachine) LoadProgram(p io.Reader) error {
	// read all data into byte slice
	programData, err := ioutil.ReadAll(p)
	if err != nil {
		return fmt.Errorf("error reading data: %s", err)
	}
	// convert to uint32 ("platters")
	numPlatters := int(math.Ceil(float64(len(programData)) / 4))
	platterProgram := make([]platter, numPlatters)
	err = binary.Read(bytes.NewBuffer(programData), binary.BigEndian, &platterProgram)
	if err != nil {
		return fmt.Errorf("error converting bytes to platters: %s", err)
	}
	// allocate "zero array"
	ref := um.allocateHeapArray(platter(len(platterProgram)))
	// and copy program into it
	copy(um.Heap[ref], platterProgram)
	return nil
}

func (um *UniversalMachine) Run() {
	go um.spin()
}
func (um *UniversalMachine) Done() <-chan error {
	return um.done
}
func (um *UniversalMachine) halt(err error) {
	um.done <- err
	close(um.done)
}
func (um *UniversalMachine) allocateHeapArray(len platter) platter {
	um.Heap[um.NextHeap] = make([]platter, len)
	um.NextHeap++
	return um.NextHeap - 1
}

func (um *UniversalMachine) spin() {
	var instruction platter
	var op, valInRegB platter
	programHeap := um.Heap[0]
	for {
		instruction = programHeap[um.ExFinger]
		um.ExFinger++

		op = instruction & 0xf0000000

		switch op {
		case 0:
			// #0. Conditional Move.
			// The register A receives the value in register B,
			// unless the register C contains 0.
			if um.Gpr[instruction&0x00000007] != 0 {
				um.Gpr[(instruction&0x000001C0)>>6] = um.Gpr[(instruction&0x00000038)>>3]
			}

		case 1 << 28:
			// #1. Array Index.
			// The register A receives the value stored at offset
			// in register C in the array identified by B.
			um.Gpr[(instruction&0x000001C0)>>6] = um.Heap[um.Gpr[(instruction&0x00000038)>>3]][um.Gpr[instruction&0x00000007]]
		case 2 << 28:
			// #2. Array Amendment.
			// The array identified by A is amended at the offset
			// in register B to store the value in register C.
			um.Heap[um.Gpr[((instruction&0x000001C0)>>6)]][um.Gpr[(instruction&0x00000038)>>3]] = um.Gpr[instruction&0x00000007]
		case 3 << 28:
			// #3. Addition.
			// The register A receives the value in register B plus
			// the value in register C, modulo 2^32.
			um.Gpr[(instruction&0x000001C0)>>6] = um.Gpr[(instruction&0x00000038)>>3] + um.Gpr[instruction&0x00000007]
		case 4 << 28:
			// #4. Multiplication.
			// The register A receives the value in register B times
			// the value in register C, modulo 2^32.
			um.Gpr[(instruction&0x000001C0)>>6] = um.Gpr[(instruction&0x00000038)>>3] * um.Gpr[instruction&0x00000007]
		case 5 << 28:
			// #5. Division.
			// The register A receives the value in register B
			// divided by the value in register C, if any, where
			// each quantity is treated treated as an unsigned 32
			// bit number.
			um.Gpr[(instruction&0x000001C0)>>6] = um.Gpr[(instruction&0x00000038)>>3] / um.Gpr[instruction&0x00000007]
		case 6 << 28:
			// #6. Not-And.
			// Each bit in the register A receives the 1 bit if
			// either register B or register C has a 0 bit in that
			// position.  Otherwise the bit in register A receives
			// the 0 bit.
			um.Gpr[(instruction&0x000001C0)>>6] = ^(um.Gpr[(instruction&0x00000038)>>3] & um.Gpr[instruction&0x00000007])
		case 7 << 28:
			// #7. Halt.
			// The universal machine stops computation.
			um.halt(nil)
			return
		case 8 << 28:
			// #8. Allocation.
			// A new array is created with a capacity of platters
			// commensurate to the value in the register C. This
			// new array is initialized entirely with platters
			// holding the value 0. A bit pattern not consisting of
			// exclusively the 0 bit, and that identifies no other
			// active allocated array, is placed in the B register.
			um.Gpr[(instruction&0x00000038)>>3] = platter(um.allocateHeapArray(um.Gpr[(instruction & 0x00000007)]))
		case 9 << 28:
			// #9. Abandonment.
			// The array identified by the register C is abandoned.
			// Future allocations may then reuse that identifier.
			delete(um.Heap, um.Gpr[instruction&0x00000007])
		case 10 << 28:
			// #10. Output.
			// The value in the register C is displayed on the console
			// immediately. Only values between and including 0 and 255
			// are allowed.
			um.consoleOut.Write([]byte{uint8(um.Gpr[instruction&0x00000007])})
		case 11 << 28:
			// #11. Input.
			// The universal machine waits for input on the console.
			// When input arrives, the register C is loaded with the
			// input, which must be between and including 0 and 255.
			// If the end of input has been signaled, then the
			// register C is endowed with a uniform value pattern
			// where every place is pregnant with the 1 bit.

			// Trigger backup before accepting input. Done here as a good place
			// while everything is stopped so we don't need to use mutexes, or
			// keep a count of the number of cycles. i.e. when we're expecting
			// input, it doesn't matter if we take a few extra milliseconds to
			// process, whereas any other solution means adding cpu time to
			// every cycle.
			um.backup()

			ip := make([]byte, 1)
			count, err := um.consoleIn.Read(ip)
			if count == 1 {
				um.Gpr[(instruction & 0x00000007)] = platter(ip[0])
			}
			if err != nil {
				if err == io.EOF {
					um.Gpr[(instruction & 0x00000007)] = 0xffffffff
				} else {
					um.halt(fmt.Errorf("error reading from stdin: %s", err))
					return
				}
			}
		case 12 << 28:
			// #12. Load Program.
			// The array identified by the B register is duplicated
			// and the duplicate shall replace the '0' array,
			// regardless of size. The execution finger is placed
			// to indicate the platter of this array that is
			// described by the offset given in C, where the value
			// 0 denotes the first platter, 1 the second, et
			// cetera.
			//
			// The '0' array shall be the most sublime choice for
			// loading, and shall be handled with the utmost
			// velocity.

			// allocate new program array
			valInRegB = um.Gpr[(instruction&0x00000038)>>3]
			if valInRegB != 0 {
				newProgram := make([]platter, len(um.Heap[valInRegB]))
				// copy data
				copy(newProgram, um.Heap[valInRegB])
				// set to zero array
				um.Heap[0] = newProgram
				programHeap = um.Heap[0]
			}
			// set execution finger
			um.ExFinger = int(um.Gpr[(instruction & 0x00000007)])
		case 13 << 28:
			//         A
			//         |
			//         vvv
			//    .--------------------------------.
			//    |VUTSRQPONMLKJIHGFEDCBA9876543210|
			//    `--------------------------------'
			//     ^^^^   ^^^^^^^^^^^^^^^^^^^^^^^^^
			//     |      |
			//     |      value
			//     |
			//     operator number

			// #13. Orthography.
			// The value indicated is loaded into the register A
			// forthwith.
			um.Gpr[(instruction&0x0e000000)>>25] = (instruction & 0x01ffffff)

		}
	}
}
