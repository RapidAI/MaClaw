/* gcc -std=gnu11 -Wall -Werror -I../../main -o test_layout test_layout.c ../../main/buddy_layout.c */
#include "buddy_layout.h"

#include <stdio.h>
#include <stdlib.h>

static void expect(int cond, const char *msg) {
    if (cond) return;
    fprintf(stderr, "fail: %s\n", msg);
    exit(1);
}

int main(void) {
    buddy_layout_t round;
    buddy_layout_t square;
    buddy_layout_metrics(true, &round);
    buddy_layout_metrics(false, &square);

    expect(round.label_x == 88, "round labels stay on the circle inset");
    expect(round.value_right == 262, "round values end before the bezel");
    expect(round.title_w == 240, "round titles use the lower chord");
    /* A 24px status line whose top is y=270. The bottom corners are the
     * tight side of the circle. */
    const int tail_dy = 270 + 24 - 180;
    const int tail_half = round.tail_w / 2;
    expect(tail_half * tail_half + tail_dy * tail_dy <= 150 * 150,
           "round status line stays inside the aperture");
    expect(round.arc_standby, "round standby follows the rim");
    expect(round.index_y == 186, "round index stays beside the counters");
    /* "n/n" is three ASCII cells at scale 1 (6px each). It must clear the
     * scale-4 counters, which end at value_right. */
    expect(round.index_right - 18 >= round.value_right + 4, "round index clears the counter glyphs");
    const int ix = round.index_right - 180;
    const int iy = round.index_y - 180;
    expect(ix * ix + iy * iy <= 150 * 150, "round index stays inside the aperture");

    expect(square.label_x <= 24, "square labels use the side margin");
    expect(square.value_right >= 336, "square values reach the opposite margin");
    expect(square.title_w >= 300, "square titles use the rectangle width");
    expect(square.tail_w >= 300, "square status line uses the rectangle width");
    expect(!square.arc_standby, "square standby is straight text");
    /* Scale-4 counter glyphs are 28px tall and right-aligned to value_right.
     * The index must not share that box when it uses the same right edge. */
    expect(square.index_y + 8 <= 166 || square.index_y >= 166 + 28,
           "square index clears the counter glyphs");

    const int dx = square.label_x - 180;
    const int dy = 74 - 180;
    expect(dx * dx + dy * dy > 150 * 150, "square label origin is outside the round aperture");

    const int rx = round.label_x - 180;
    const int ry = 74 - 180;
    expect(rx * rx + ry * ry <= 150 * 150, "round label origin stays inside the aperture");

    printf("layout metrics: ok\n");
    return 0;
}
