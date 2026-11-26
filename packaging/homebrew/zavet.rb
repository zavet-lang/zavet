class Zavet < Formula
  desc "Язык программирования Завет на библейском русском"
  homepage "https://github.com/zavet-lang/zavet"
  version "1.1.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/zavet-lang/zavet/releases/download/v#{version}/zavet_#{version}_darwin_arm64.tar.gz"
    else
      url "https://github.com/zavet-lang/zavet/releases/download/v#{version}/zavet_#{version}_darwin_amd64.tar.gz"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/zavet-lang/zavet/releases/download/v#{version}/zavet_#{version}_linux_arm64.tar.gz"
    else
      url "https://github.com/zavet-lang/zavet/releases/download/v#{version}/zavet_#{version}_linux_amd64.tar.gz"
    end
  end

  def install
    bin.install "zavet"
  end

  test do
    assert_match "Завет", shell_output("#{bin}/zavet версия")
  end
end
