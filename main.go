package main

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"math/rand"
	"os"
	"strings"
	"text/template"
)

type logFormatter struct {
	node        string
	environment string
}

func (l *logFormatter) Format(entry *log.Entry) ([]byte, error) {
	return []byte(fmt.Sprintf("[%v] %v", entry.Level, entry.Message)), nil
}

func setLogFormatter() {
	log.SetFormatter(&logFormatter{})
}

func rndCond(rnd *rand.Rand, numFuncParams int, maxConst uint64, additionalInputs []string) string {
	rndInput := func() string {
		numAdditionalInputs := len(additionalInputs)
		idx := rnd.Intn(numFuncParams + numAdditionalInputs)
		if idx < numFuncParams {
			return fmt.Sprintf("p%v", idx)
		}
		idx -= numFuncParams
		return fmt.Sprintf("uint64(%v)", additionalInputs[idx])
	}
	rndConst := func() string {
		c := rnd.Uint64()
		if maxConst < c {
			c = c % (maxConst + 1)
		}
		return fmt.Sprintf("uint64(%v)", c)
	}
	rndExprLeft := fmt.Sprintf("%v", rndInput())
	var rndExprRight string
	switch rnd.Intn(6) {
	case 0:
		rndExprRight = rndConst()
	case 1:
		rndExprRight = fmt.Sprintf("%v", rndInput())
	case 2:
		rndExprRight = fmt.Sprintf("uint64(%v + %v)", rndConst(), rndInput())
	case 3:
		rndExprRight = fmt.Sprintf("uint64(%v + %v)", rndInput(), rndInput())
	case 4:
		rndExprRight = fmt.Sprintf("uint64(%v * %v)", rndConst(), rndInput())
	default:
		rndExprRight = fmt.Sprintf("uint64(%v * %v)", rndInput(), rndInput())
	}
	ops := []string{"<", ">", "<=", ">=", "==", "!="}
	numOps := len(ops)
	rndOp := ops[rnd.Intn(numOps)]
	return fmt.Sprintf("%v %v %v", rndExprLeft, rndOp, rndExprRight)
}

func generateBenchmarkStateless() error {
	seed := int64(0)
	numFuncParams := 8
	maxConst := uint64(64)
	retProb := 0.1
	maxDepth := 8
	additionalInputs := []string{
		"msg.value",
		"tx.gasprice",
		"block.number",
	}

	params := make([]string, numFuncParams)
	for i := 0; i < numFuncParams; i++ {
		params[i] = fmt.Sprintf("uint64 p%v", i)
	}
	paramsStr := strings.Join(params, ", ")

	retCnt := 0
	rnd := rand.New(rand.NewSource(seed))
	var rndTreeStmtBlock func(maxDepth, indent int) string
	rndTreeStmtBlock = func(maxDepth, indent int) string {
		if maxDepth < 1 || rnd.Float64() < retProb {
			rv := retCnt
			retCnt++
			indentStr := strings.Repeat("  ", indent)
			return fmt.Sprintf(
				`%vemit AssertionFailed("%v"); assert(false); return %v;`,
				indentStr,
				rv,
				rv,
			)
		}
		indentStr := strings.Repeat("  ", indent)
		rCond := rndCond(rnd, numFuncParams, maxConst, additionalInputs)
		thBlock := rndTreeStmtBlock(maxDepth-1, indent+1)
		elBlock := rndTreeStmtBlock(maxDepth-1, indent+1)
		return fmt.Sprintf(
			`%vif (%v) {
%v
%v} else {
%v
%v}`,
			indentStr,
			rCond,
			thBlock,
			indentStr,
			elBlock,
			indentStr,
		)
	}
	body := rndTreeStmtBlock(maxDepth, 3)

	type ProgramPlaceholders struct {
		ParamsStr string
		ArgsStr   string
		DimX      int
		DimY      int
		BodyStr   string
		RetCnt    int
	}
	placeholders := ProgramPlaceholders{
		ParamsStr: paramsStr,
		BodyStr:   body,
	}
	progTemplate := `pragma solidity ^0.8.19;
/// automatically generated by Daedaluzz
contract C {
  event AssertionFailed(string message);
  function f({{.ParamsStr}}) payable external returns (uint64) {
    unchecked {
{{.BodyStr}}
    }
  }
}
`
	parsedTemplate, parseErr := template.New("program").Parse(progTemplate)
	if parseErr != nil {
		return parseErr
	}
	outFile, createErr := os.Create("generated-maze.sol")
	if createErr != nil {
		return createErr
	}
	defer outFile.Close()
	execErr := parsedTemplate.Execute(outFile, placeholders)
	if execErr != nil {
		return execErr
	}
	return nil
}

func generateBenchmarkStateful() error {
	seed := int64(0)
	numFuncParams := 8
	maxConst := uint64(64)
	dimX := 7
	dimY := 7
	minDepth := 2
	maxDepth := 16
	useAdditionalInputs := false
	var additionalInputs []string
	if useAdditionalInputs {
		additionalInputs = []string{
			"msg.value",
			"tx.gasprice",
			"block.number",
		}
	}

	params := make([]string, numFuncParams)
	for i := 0; i < numFuncParams; i++ {
		params[i] = fmt.Sprintf("uint64 p%v", i)
	}
	paramsStr := strings.Join(params, ", ")

	args := make([]string, numFuncParams)
	for i := 0; i < numFuncParams; i++ {
		args[i] = fmt.Sprintf("p%v", i)
	}
	argsStr := strings.Join(args, ", ")

	rnd := rand.New(rand.NewSource(seed))
	var rndLinearStmtBlock func(depth, indent, rv int) string
	rndLinearStmtBlock = func(depth, indent, rv int) string {
		if depth < 1 {
			indentStr := strings.Repeat("  ", indent)
			return fmt.Sprintf(
				`%vemit AssertionFailed("%v"); assert(false);  // bug`,
				indentStr,
				rv,
			)
		}
		indentStr := strings.Repeat("  ", indent)
		rCond := rndCond(rnd, numFuncParams, maxConst, additionalInputs)
		thBlock := rndLinearStmtBlock(depth-1, indent+1, rv)
		return fmt.Sprintf(
			`%vif (%v) {
%v
%v}`,
			indentStr,
			rCond,
			thBlock,
			indentStr,
		)
	}

	retCnt := 0
	var bodyBuilder strings.Builder
	for i := 0; i < dimX; i++ {
		for j := 0; j < dimY; j++ {
			rv := retCnt
			retCnt++
			bl := "        // start"
			if 0 < i || 0 < j {
				d := rnd.Intn(maxDepth + 1)
				if minDepth <= d {
					bl = rndLinearStmtBlock(d, 4, rv)
				} else {
					bl = "        require(false);  // wall"
				}
			}
			_, _ = fmt.Fprintf(
				&bodyBuilder,
				`      if (x == %v && y == %v) {
%v
        return %v;
      }
`,
				i,
				j,
				bl,
				rv,
			)
		}
	}
	bodyStr := bodyBuilder.String()

	type ProgramPlaceholders struct {
		ParamsStr string
		ArgsStr   string
		DimX      int
		DimY      int
		BodyStr   string
		RetCnt    int
	}
	placeholders := ProgramPlaceholders{
		ParamsStr: paramsStr,
		ArgsStr:   argsStr,
		DimX:      dimY,
		DimY:      dimY,
		BodyStr:   bodyStr,
		RetCnt:    retCnt,
	}
	progTemplate := `pragma solidity ^0.8.19;
/// automatically generated by Daedaluzz
contract Maze {
  event AssertionFailed(string message);
  uint64 private x;
  uint64 private y;
  function moveNorth({{.ParamsStr}}) payable external returns (int64) {
    uint64 ny = y + 1;
    require(ny < {{.DimY}});
    y = ny;
    return step({{.ArgsStr}});
  }
  function moveSouth({{.ParamsStr}}) payable external returns (int64) {
    require(0 < y);
    uint64 ny = y - 1;
    y = ny;
    return step({{.ArgsStr}});
  }
  function moveEast({{.ParamsStr}}) payable external returns (int64) {
    uint64 nx = x + 1;
    require(nx < {{.DimX}});
    x = nx;
    return step({{.ArgsStr}});
  }
  function moveWest({{.ParamsStr}}) payable external returns (int64) {
    require(0 < x);
    uint64 nx = x - 1;
    x = nx;
    return step({{.ArgsStr}});
  }
  function step({{.ParamsStr}}) internal returns (int64) {
    unchecked {
{{.BodyStr}}      return {{.RetCnt}};
    }
  }
}
`
	parsedTemplate, parseErr := template.New("program").Parse(progTemplate)
	if parseErr != nil {
		return parseErr
	}
	outFile, createErr := os.Create("generated-maze.sol")
	if createErr != nil {
		return createErr
	}
	defer outFile.Close()
	execErr := parsedTemplate.Execute(outFile, placeholders)
	if execErr != nil {
		return execErr
	}
	return nil
}

func run(ctx *cli.Context) error {
	setLogFormatter()
	log.SetOutput(os.Stderr)
	level := log.InfoLevel
	log.SetLevel(level)

	makeStateful := true
	if makeStateful {
		return generateBenchmarkStateful()
	}
	return generateBenchmarkStateless()
}

//goland:noinspection GoUnhandledErrorResult
func main() {
	defer func() {
		if r := recover(); r != nil {
			os.Exit(2)
		}
	}()
	app := cli.NewApp()
	app.Flags = []cli.Flag{}
	app.Version = "0.0.1"
	app.Usage = "Daedaluzz"
	app.Action = run
	if err := app.Run(os.Args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "terminated with error: %v\n", err)
		os.Exit(1)
	}
}
