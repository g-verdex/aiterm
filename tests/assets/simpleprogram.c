#include <stdio.h>
#include <string.h>
#include <stdlib.h>

static int check(const char *s) {
    if (!s) return -1;
    size_t n = strlen(s);
    if (n != 5) return 1;
    if (s[0] == 'p' && s[1] == 'a' && s[2] == 'n' && s[3] == 'c' && s[4] == 'a') {
        return 0;
    }
    return 2;
}

int main(int argc, char **argv) {
    const char *in = argc > 1 ? argv[1] : "";
    int r = check(in);
    if (r == 0) {
        puts("OK");
    } else {
        puts("NOPE");
    }
    return r;
}

