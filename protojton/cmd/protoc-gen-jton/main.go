// Command protoc-gen-jton is a protoc/buf plugin that generates a JTON codec for
// each message: a streaming MarshalJTON and a tree-based UnmarshalJTON that read
// and write Go struct fields directly, with no protoreflect on the hot path.
//
// Generated files are written next to the .pb.go as <name>.pb.jton.go in the
// same package, so the generated methods attach to the message types.
package main

import (
	"github.com/birdayz/jton-go/protojton/internal/generator"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"
)

func main() {
	protogen.Options{}.Run(func(gen *protogen.Plugin) error {
		gen.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
		for _, f := range gen.Files {
			if f.Generate {
				generator.GenerateFile(gen, f)
			}
		}
		return nil
	})
}
