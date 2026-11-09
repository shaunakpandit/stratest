{ pkgs, ... }:

{
  languages.go.enable = true;
  dotenv.enable = true;
  packages = with pkgs; [
    golangci-lint
    gotools
  ];
}
