#include <fmt/format.h>
#include <fmt/color.h>

int main() {
    fmt::print("Hello from {}\n", "sea package manager");
    fmt::print(fg(fmt::color::green), "All {} packages loaded\n", 3);
    fmt::print("fmt version: {}.{}.{}\n",
               FMT_VERSION / 10000, FMT_VERSION / 100 % 100, FMT_VERSION % 100);
    return 0;
}
