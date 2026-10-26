# Homebrew formula for Matthew-Eucaristo/homebrew-tap (Formula/flutterkill.rb).
# Builds from the tagged source tarball. Bump `url` and `sha256` per release;
# see RELEASING.md.
class Flutterkill < Formula
  desc "Reclaim disk space from Flutter/Dart build artifacts and caches"
  homepage "https://github.com/Matthew-Eucaristo/flutterkill"
  url "https://github.com/Matthew-Eucaristo/flutterkill/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "REPLACE_WITH_SHA256_OF_THE_TAG_TARBALL"
  license "MIT"
  head "https://github.com/Matthew-Eucaristo/flutterkill.git", branch: "main"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-X main.version=#{version}")
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/flutterkill --version")

    (testpath/"app/pubspec.yaml").write "name: demo_app\n"
    (testpath/"app/build").mkpath
    output = shell_output("#{bin}/flutterkill --json --no-global #{testpath}")
    assert_match "demo_app", output
    assert_path_exists testpath/"app/build" # --json never removes anything
  end
end
