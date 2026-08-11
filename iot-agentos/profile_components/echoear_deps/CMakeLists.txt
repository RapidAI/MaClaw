# This component is intentionally source-free. Selecting it through the
# profile build wrapper exposes EchoEar's controller dependency before the
# main component's Kconfig-gated REQUIRES list is evaluated.
idf_component_register()
