{
  description = "QR code and barcode encoder/decoder for the terminal";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: rec {
        qrx = pkgs.buildGoModule {
          pname = "qrx";
          version = "0.1.0";
          src = self;

          vendorHash = "sha256-/3tvdDLu9UsSZdkaQvZmaKtWenRwGypmqRaIxZMmHGQ=";

          ldflags = [
            "-s"
            "-w"
          ];

          meta = {
            description = "QR code and barcode encoder/decoder for the terminal";
            homepage = "https://github.com/jim-ww/qrx";
            license = nixpkgs.lib.licenses.gpl3Only;
            mainProgram = "qrx";
          };
        };
        default = qrx;
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.gopls
            pkgs.golangci-lint
          ];
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-tree);
    };
}
