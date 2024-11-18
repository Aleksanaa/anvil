package main

// This is a tool to help darken or lighten the colors in an Anvil .js style file.
// It is a simple tool. Give it a style file or portion of one on stdin, and it will
// search for hex colors of the form "#xxxxxx" where x are hex digits, and replace them
// with a new color. The result is printed on stdout.
import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/crazy3lf/colorconv"
	"github.com/spf13/pflag"
)

var optDarken = pflag.StringP("darken", "d", "", "Darken the colors. This option accepts a constant amount to reduce the Value of the color (in HSV) as a float, or a percentage as a number with a following % sign")
var optLighten = pflag.StringP("lighten", "l", "", "Lighten the colors. This option accepts a constant amount to reduce the Value of the color (in HSV) as a float, or a percentage as a number with a following % sign")

func main() {
	pflag.Usage = usage
	pflag.Parse()

	oper = newOp()

	scanner := bufio.NewScanner(os.Stdin)
	lineno := 1
	for scanner.Scan() {
		line := scanner.Text()
		line = updateLine(line, lineno)
		fmt.Printf("%s\n", line)
		lineno++
	}
}

var oper op

type op struct {
	opcode opcode
	amt    float64
	isPct  bool
}

type opcode int

const (
	darken opcode = iota
	lighten
)

func newOp() (o op) {
	var amt string
	if *optDarken != "" {
		o.opcode = darken
		amt = *optDarken
	} else if *optLighten != "" {
		o.opcode = lighten
		amt = *optLighten
	} else {
		fmt.Fprintf(os.Stderr, "error: one of --lighten or --darken is needed\n")
		os.Exit(1)
	}

	l := len(amt)
	if amt[l-1] == '%' {
		o.isPct = true
		amt = amt[:l-1]
	}

	v, err := strconv.ParseFloat(amt, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing argument to --lighten or --darken\n")
		os.Exit(1)
	}

	o.amt = v
	return
}

func (o op) update(h, s, v *float64) {
	scaleByPct := func(f float64) float64 {
		if o.opcode == darken {
			//fmt.Printf("darken %v by %v\n", f, o.amt)
			return f - f*(o.amt/100)
		} else {
			return f + f*(o.amt/100)
		}
	}

	//oldv := *v
	if o.isPct {
		*v = scaleByPct(*v)
	} else {
		*v -= o.amt
	}

	//fmt.Printf("update: %f -> %f\n", oldv, *v)
}

var colorRegex = regexp.MustCompile(`"#[[:xdigit:]]{6}"`)

func updateLine(line string, lineno int) (newline string) {
	//matches := colorRegex.FindAllStringSubmatchIndex(line)
	newline = line
	skipLeading := 0
	//for _, match := range matches {
	for {
		text := newline[skipLeading:]
		match := colorRegex.FindStringSubmatchIndex(text)
		if match == nil {
			break
		}
		//fmt.Printf("match: %#v\n", match)
		colortext := text[match[0]:match[1]]
		digits := colortext[2 : len(colortext)-1]
		r, g, b, err := colorconv.HexToRGB(digits)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error on line %d, col %d: can't parse color %s: %v", lineno, match[0]+1, colortext, err)
			continue
		}

		h, s, v := colorconv.RGBToHSV(r, g, b)
		updateColor(&h, &s, &v)
		r, g, b, err = colorconv.HSVToRGB(h, s, v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error on line %d, col %d: can't convert color %s back from HSV to RGB: %v", lineno, match[0]+1, colortext, err)
			continue
		}
		colortext = fmt.Sprintf(`"#%02x%02x%02x"`, r, g, b)

		text = text[0:match[0]] + colortext + text[match[1]:]
		newline = newline[0:skipLeading] + text
		skipLeading += match[0] + len(colortext)
		//fmt.Printf("line now: %s\n", newline)
	}
	return
}

func updateColor(h, s, v *float64) {
	oper.update(h, s, v)
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s <--darken|-d N | --lighten|-l N>\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "This is a tool to help darken or lighten the colors in an Anvil .js style file. It is a simple tool. Give it a style file or portion of one on stdin, and it will search for hex colors of the form '#xxxxxx' where x are hex digits, and replace them with a new color. The result is printed on stdout.\n\nUse one of the --lighten or --darken arguments to choose how to modify the colors")

	pflag.PrintDefaults()
}
