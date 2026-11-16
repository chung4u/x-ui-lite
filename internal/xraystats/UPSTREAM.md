# Xray statistics wire interface

These unmodified protobuf and gRPC generated files come from XTLS/Xray-core v1.4.2 (`app/stats/command`), the exact interface previously imported by this panel. Only wire types and stubs are retained; no Xray server implementation is embedded. The running core remains an independent executable.

Source: https://github.com/XTLS/Xray-core/tree/v1.4.2/app/stats/command

License: Mozilla Public License 2.0, see LICENSE.

This isolates the stable statistics API from obsolete core runtime dependencies while allowing maintained gRPC/protobuf libraries. Compare field numbers and service method names when updating.

```json
{
  "command.proto": "96a4c87d74ddc4113b51fcc4a9cf90c21ae20e99854b0ba3ddc2f404f33b9d0a",
  "command.pb.go": "3c1e5b1e847739051c04ff8e89579728aec4a66a598609f6d27ae4f59f5a2d5b",
  "command_grpc.pb.go": "999f69e3ec331aa0c226f6962698d739c9a6b5ab1ee17a2b2f23bab5c48031d6"
}
```
