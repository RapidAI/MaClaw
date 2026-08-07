import serial, time
port=serial.Serial("COM3",115200,timeout=.2)
end=time.time()+80
chunks=[]
while time.time()<end:
    data=port.read(4096)
    if data: chunks.append(data)
port.close()
open("serial-com3-pet-exclusive-verify.log","wb").write(b"".join(chunks))
print(sum(map(len,chunks)))
