//go:build ignore

// test_edit_input.go - Test file for edit tool
// 该文件是 Edit 工具的文本夹具（内容会被工具读写），不参与编译。
package main

import "fmt"

func main() {
	fmt.Println("Hello")
	fmt.Println("World")
}
func test(params string) {
	fmt.Println("->" + params)
}
func test2() {

}
