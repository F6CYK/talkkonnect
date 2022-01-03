/*
 * talkkonnect headless mumble client/gateway with lcd screen and channel control
 * Copyright (C) 2018-2019, Suvir Kumar <suvir@talkkonnect.com>
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/.
 *
 * Software distributed under the License is distributed on an "AS IS" basis,
 * WITHOUT WARRANTY OF ANY KIND, either express or implied. See the License
 * for the specific language governing rights and limitations under the
 * License.
 *
 * talkkonnect is the based on talkiepi and barnard by Daniel Chote and Tim Cooper
 *
 * The Initial Developer of the Original Code is
 * Suvir Kumar <suvir@talkkonnect.com>
 * Portions created by the Initial Developer are Copyright (C) Suvir Kumar. All Rights Reserved.
 *
 * Contributor(s):
 *
 * Suvir Kumar <suvir@talkkonnect.com>
 *
 * My Blog is at www.talkkonnect.com
 * The source code is hosted at github.com/talkkonnect
 *
 * gps.go -> talkkonnect function to interface to USB GPS Neo6M
 */

package talkkonnect

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/adrianmo/go-nmea"
	"github.com/jacobsa/go-serial/serial"
	hd44780 "github.com/talkkonnect/go-hd44780"
)

type GSVDataStruct struct {
	PRNNumber int64
	SNR       int64
	Azimuth   int64
}

type GNSSDataStruct struct {
	DateTime   time.Time
	Date       string
	Variation  float64
	Time       string
	Validity   string
	Lattitude  float64
	Longitude  float64
	Speed      float64
	Course     float64
	FixQuality string
	SatsInUse  int64
	SatsInView int64
	HDOP       float64
	Altitude   float64
	RMCRaw     string
	GSVData    [4]GSVDataStruct
}

//global variables for gps
var (
	GNSSData       GNSSDataStruct
	GNSSDataPublic = make(chan GNSSDataStruct, Receivers+1)
)

//local variables for gps
var (
	RMCSentenceValid bool
	GGASentenceValid bool
	GSVSentenceValid bool
	goodGPSRead      bool
)

func getGpsPosition(verbosity int) (bool, error) {
	RMCSentenceValid = false
	GGASentenceValid = false
	GSVSentenceValid = false
	goodGPSRead = false

	if Config.Global.Hardware.GPS.Enabled {

		if Config.Global.Hardware.GPS.Port == "" {
			return false, errors.New("gnss port not specified")
		}

		if Config.Global.Hardware.GPS.Even && Config.Global.Hardware.GPS.Odd {
			return false, errors.New("can't specify both even and odd parity")
		}

		parity := serial.PARITY_NONE

		if Config.Global.Hardware.GPS.Even {
			parity = serial.PARITY_EVEN
		} else if Config.Global.Hardware.GPS.Odd {
			parity = serial.PARITY_ODD
		}

		options := serial.OpenOptions{
			PortName:               Config.Global.Hardware.GPS.Port,
			BaudRate:               Config.Global.Hardware.GPS.Baud,
			DataBits:               Config.Global.Hardware.GPS.DataBits,
			StopBits:               Config.Global.Hardware.GPS.StopBits,
			MinimumReadSize:        Config.Global.Hardware.GPS.MinRead,
			InterCharacterTimeout:  Config.Global.Hardware.GPS.CharTimeOut,
			ParityMode:             parity,
			Rs485Enable:            Config.Global.Hardware.GPS.Rs485,
			Rs485RtsHighDuringSend: Config.Global.Hardware.GPS.Rs485HighDuringSend,
			Rs485RtsHighAfterSend:  Config.Global.Hardware.GPS.Rs485HighAfterSend,
		}

		f, err := serial.Open(options)

		if err != nil {
			Config.Global.Hardware.GPS.Enabled = false
			return false, errors.New("cannot open serial port")
		} else {
			defer f.Close()
		}

		if Config.Global.Hardware.GPS.TxData != "" {
			txData_, err := hex.DecodeString(Config.Global.Hardware.GPS.TxData)

			if err != nil {
				Config.Global.Hardware.GPS.Enabled = false
				return false, errors.New("cannot decode hex data")
			}

			log.Println("debug: Sending To Serial ", hex.EncodeToString(txData_))

			count, err := f.Write(txData_)

			if err != nil {
				return false, errors.New("error writing to serial port")
			} else {
				log.Printf("debug: Wrote %v Bytes To Serial\n", count)
			}

		}

		if Config.Global.Hardware.GPS.Rx {
			serialPort, err := serial.Open(options)
			if err != nil {
				log.Println("warn: Unable to Open Serial Port Error ", err)
			}

			defer serialPort.Close()

			reader := bufio.NewReader(serialPort)
			scanner := bufio.NewScanner(reader)

			for scanner.Scan() {
				s, err := nmea.Parse(scanner.Text())
				if err == nil {

					switch s.DataType() {

					case nmea.TypeRMC:
						{
							m := s.(nmea.RMC)
							if m.Latitude != 0 && m.Longitude != 0 && !RMCSentenceValid {
								RMCSentenceValid = true
								GNSSData.DateTime = time.Now().UTC()
								GNSSData.Date = fmt.Sprintf("%v", m.Date)
								GNSSData.Time = fmt.Sprintf("%v", m.Time)
								GNSSData.Validity = fmt.Sprintf("%v", m.Validity)
								GNSSData.Lattitude = m.Latitude
								GNSSData.Longitude = m.Longitude
								GNSSData.Speed = m.Speed
								GNSSData.Course = m.Course
								GNSSData.Variation = m.Variation
								GNSSData.RMCRaw = m.Raw
							}
						}
					case nmea.TypeGGA:
						{
							m := s.(nmea.GGA)
							if m.Latitude != 0 && m.Longitude != 0 && !GGASentenceValid {
								GGASentenceValid = true
								GNSSData.FixQuality = m.FixQuality
								GNSSData.SatsInUse = m.NumSatellites
								GNSSData.HDOP = m.HDOP
								GNSSData.Altitude = m.Altitude
							}
						}

					case nmea.TypeGSV:
						{
							m := s.(nmea.GSV)
							for i := range m.Info {
								if m.Info[i].SNR > 0 && !GSVSentenceValid {
									GNSSData.GSVData[i].PRNNumber = s.(nmea.GSV).Info[i].SVPRNNumber
									GNSSData.GSVData[i].SNR = s.(nmea.GSV).Info[i].SNR
									GNSSData.GSVData[i].Azimuth = s.(nmea.GSV).Info[i].Azimuth
									if i >= 3 {
										GSVSentenceValid = true
										GNSSData.SatsInView = m.NumberSVsInView
									}
								}
							}
						}
					}
				}
			}

			if RMCSentenceValid && GGASentenceValid && GSVSentenceValid {
				goodGPSRead = true
				log.Printf("debug: GPS Good Read Broadcasting to %v receivers\n", Receivers)
				for a := 0; a < Receivers; a++ {
					GNSSDataPublic <- GNSSData
					time.Sleep(100 * time.Millisecond)
				}
			}

		} else {
			return false, errors.New("error parsing gnss module")
		}
		return goodGPSRead, nil
	}
	return false, errors.New("gnss not enabled")
}

func httpSendTraccarOsman() {
	Receivers++
	for {
		GNSSDataTraccar := <-GNSSDataPublic

		TraccarDateTime := GNSSDataTraccar.DateTime.Format("2006-02-01") + "%20" + GNSSDataTraccar.DateTime.Format("15:04:05")

		TraccarServerFullURLOsman := (fmt.Sprint(Config.Global.Hardware.Traccar.Protocol.Osmand.ServerURL) + ":" + fmt.Sprint(Config.Global.Hardware.Traccar.Protocol.Osmand.Port) + "/?" + "id=" + Config.Global.Hardware.Traccar.ClientId + "&" +
			"timestamp=" + TraccarDateTime + "&" + "lat=" + fmt.Sprintf("%f", GNSSDataTraccar.Lattitude) +
			"&" + "lon=" + fmt.Sprintf("%f", GNSSDataTraccar.Longitude) + "&" + "speed=" + fmt.Sprintf("%f", GNSSDataTraccar.Speed) + "&" + "course=" +
			fmt.Sprintf("%f", GNSSDataTraccar.Course) + "&" + "variation=" + fmt.Sprintf("%f", GNSSDataTraccar.Variation))

		response, err := http.Get(TraccarServerFullURLOsman)

		if err != nil {
			log.Println("error: Cannot Establish Connection with Traccar Server! Error ", err)
			return
		}

		defer response.Body.Close()
		contents, err := ioutil.ReadAll(response.Body)

		if err != nil {
			log.Println("error: Error Sending Data to Traccar Server!")
		}

		if response.ContentLength == 0 {
			log.Println("alert: Empty Request Response Body")
		} else {
			log.Printf("debug: Traccar Web Server Response -->\n-------------------------------------------------------------\n %v \n-------------------------------------------------------------\n", string(contents))
		}

		log.Println("debug: HTTP Response Status from Traccar:", response.StatusCode, http.StatusText(response.StatusCode))
		if response.StatusCode >= 200 && response.StatusCode <= 299 {
			log.Println("info: HTTP Status Code from Traccar is in the 2xx range. This is OK.")
		}

	}
}

func tcpSendT55Traccar() {
	Receivers++
	for {
		GNSSDataTraccar := <-GNSSDataPublic

		PGID := "$PGID" + "," + Config.Global.Hardware.Traccar.ClientId + "*0F" + "\r" + "\n"
		GPRMC := GNSSDataTraccar.RMCRaw + "\r" + "\n"
		log.Println("debug: $GPRMC to send is: " + GNSSDataTraccar.RMCRaw)

		CONN, _ := net.Dial("tcp", Config.Global.Hardware.Traccar.Protocol.T55.ServerIP+":"+fmt.Sprint(Config.Global.Hardware.Traccar.Protocol.T55.Port)) // Use port 5005 for T55. Keep-alive.
		err := CONN.(*net.TCPConn).SetKeepAlive(true)
		if err != nil {
			fmt.Println(err)
			return
		}
		err = CONN.(*net.TCPConn).SetKeepAlivePeriod(60 * time.Second)
		if err != nil {
			fmt.Println(err)
			return
		}
		err = CONN.(*net.TCPConn).SetNoDelay(false)
		if err != nil {
			fmt.Println(err)
			return
		}
		err = CONN.(*net.TCPConn).SetLinger(0)
		if err != nil {
			fmt.Println(err)
			return
		}

		log.Println("debug: Traccar Client:", CONN.LocalAddr().String(), "Connected to Server:", CONN.RemoteAddr().String())

		fmt.Fprint(CONN, PGID) // Send ID
		time.Sleep(1 * time.Second)
		fmt.Fprint(CONN, GPRMC) // send $GPRMC
		log.Println("debug: Sending position message to Traccar over Protocol: " + strings.Title(strings.ToLower(Config.Global.Hardware.Traccar.Protocol.Name)))

		notify := make(chan error)

		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := CONN.Read(buf)
				if err != nil {
					notify <- err
					if io.EOF == err {
						close(notify)
						return
					}
				}

				if n > 0 {
					log.Printf("alert: Unexpected Data: %s", buf[:n])
				}
			}
		}()

		for {
			select {
			case err := <-notify:
				log.Println("alert: Traccar Server Connection dropped message", err)

				if err == io.EOF {
					log.Println("alert: Connection to Traccar Server was closed")
					return
				}
			case <-time.After(time.Second * 60):
				log.Println("debug: Traccar Server Connection Timeout 60. Still Alive")
			}
		}
	}
}

func httpSendTraccarOpenGTS() {
	Receivers++
	for {
		GNSSDataTraccar := <-GNSSDataPublic

		TraccarServerFullURLOpenGTS := (fmt.Sprint(Config.Global.Hardware.Traccar.Protocol.Opengts.ServerURL) + ":" + fmt.Sprint(Config.Global.Hardware.Traccar.Protocol.Opengts.Port) + "/?id=" + Config.Global.Hardware.Traccar.ClientId + "&grmpc=" + GNSSDataTraccar.RMCRaw)

		log.Println("alert:", TraccarServerFullURLOpenGTS)

		response, err := http.Get(TraccarServerFullURLOpenGTS)

		if err != nil {
			log.Println("error: Cannot Establish Connection with Traccar Server! Error ", err)
			return
		}

		defer response.Body.Close()
		contents, err := ioutil.ReadAll(response.Body)

		if err != nil {
			log.Println("error: Error Sending Data to Traccar Server!")
		}

		if response.ContentLength == 0 {
			log.Println("alert: Empty Request Response Body")
		} else {
			log.Printf("debug: Traccar Web Server Response -->\n-------------------------------------------------------------\n %v \n-------------------------------------------------------------\n", string(contents))
		}

		log.Println("debug: HTTP Response Status from Traccar:", response.StatusCode, http.StatusText(response.StatusCode))
		if response.StatusCode >= 200 && response.StatusCode <= 299 {
			log.Println("info: HTTP Status Code from Traccar is in the 2xx range. This is OK.")
		}

	}
}

func consoleScreenLogging() {
	Receivers++
	for {
		GNSSDataTraccar := <-GNSSDataPublic
		log.Printf("debug: RMC Validity (%v), GGA GPS Quality Indicator (%v) %v/%v\n", GNSSDataTraccar.Validity, GNSSDataTraccar.FixQuality, GNSSDataTraccar.SatsInUse, GNSSDataTraccar.SatsInView)
		log.Printf("debug: RMC Date Time              %v %v\n", GNSSDataTraccar.Date, GNSSDataTraccar.Time)
		log.Printf("debug: OS  DateTime(UTC)          %v\n", GNSSDataTraccar.DateTime)
		log.Printf("debug: RMC Latitude,Longitude DMS %v,%v\n", GNSSDataTraccar.Lattitude, GNSSDataTraccar.Longitude)
		log.Printf("debug: RMC Speed, Course          %v,%v\n", GNSSDataTraccar.Speed, GNSSDataTraccar.Course)
		log.Printf("debug: RMC Variation, GGA HDOP    %v,%v\n", GNSSDataTraccar.Variation, GNSSDataTraccar.HDOP)
		log.Printf("debug: GGA Altitude               %v\n", GNSSDataTraccar.Altitude)
		for i := range GNSSData.GSVData {
			log.Printf("debug: GSV SVPRNNumber,SNR, Azimuth Sat(%v) %v,%v,%v\n", i, GNSSDataTraccar.GSVData[i].PRNNumber, GNSSDataTraccar.GSVData[i].SNR, GNSSDataTraccar.GSVData[i].Azimuth)
		}
	}
}

func localScreenLogging() {
	Receivers++
	for {
		GNSSDataTraccar := <-GNSSDataPublic
		log.Printf("debug: Device Screen Latitude : %f Longitude : %f\n", GNSSDataTraccar.Lattitude, GNSSDataTraccar.Longitude)
		if Config.Global.Hardware.LCD.Enabled {
			LcdText = [4]string{"nil", "GPS OK " + GNSSDataTraccar.DateTime.Format("15:04:05"), "lat:" + fmt.Sprintf("%f", GNSSDataTraccar.Lattitude), "lon:" + fmt.Sprintf("%f", GNSSDataTraccar.Longitude) + " s:" + fmt.Sprintf("%.2f", GNSSDataTraccar.Speed*1.852)}
			go hd44780.LcdDisplay(LcdText, LCDRSPin, LCDEPin, LCDD4Pin, LCDD5Pin, LCDD6Pin, LCDD7Pin, LCDInterfaceType, LCDI2CAddress)
		}
		if Config.Global.Hardware.OLED.Enabled {
			oledDisplay(false, 4, 1, "GPS OK "+GNSSDataTraccar.DateTime.Format("15:04:05"))
			oledDisplay(false, 5, 1, "lat: "+fmt.Sprintf("%f", GNSSDataTraccar.Lattitude))
			oledDisplay(false, 6, 1, "lon: "+fmt.Sprintf("%f", GNSSDataTraccar.Longitude))
			oledDisplay(false, 7, 1, "sp: "+fmt.Sprintf("%.2f", (GNSSDataTraccar.Speed*1.852)))
		}
	}
}
