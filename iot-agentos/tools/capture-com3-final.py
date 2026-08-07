import serial
import time

port = serial.Serial("COM3", 115200, timeout=0.25)
deadline = time.time() + 65
chunks = []
while time.time() < deadline:
    data = port.read(2048)
    if data:
        chunks.append(data)
port.close()

with open("serial-com3-final-verify.log", "wb") as output:
    output.write(b"".join(chunks))
print(sum(map(len, chunks)))
