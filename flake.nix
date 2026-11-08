{
  description = "stratest: LLM-written trading strategies, backtested in the terminal";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let pkgs = nixpkgs.legacyPackages.${system};
      in {
        packages.default = pkgs.buildGoModule {
          pname = "stratest";
          version = "0.1.0";
          src = ./.;
          # Replace with the hash printed by the first `nix build`.
          vendorHash =  "sha256-Sj7ZwyRCO19R2XU10DlDTV0Jv7jmHCU/tTUOc+JFito=";
          doCheck = false; # tests need a writable cwd; run `go test ./...` in the dev shell
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [ go gopls golangci-lint gotools ];
          shellHook = ''
            if [ -f .env ]; then set -a; . ./.env; set +a; fi
            export GOFLAGS=-mod=mod
          '';
        };
      });
}
